package discord

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/keshon/melodix/internal/command"
	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/docs"
	"github.com/keshon/melodix/internal/music/player"
	"github.com/keshon/melodix/internal/music/source_resolver"
	"github.com/keshon/melodix/internal/storage"
	"github.com/keshon/melodix/pkg/cmd"

	"github.com/bwmarrin/discordgo"
)

// Bot is a Discord bot
type Bot struct {
	dg             *discordgo.Session
	storage        *storage.Storage
	slashCmds      map[string][]*discordgo.ApplicationCommand
	cfg            *config.Config
	mu             sync.RWMutex
	players        map[string]*player.Player
	sourceResolver *source_resolver.SourceResolver
}

// NewBot creates a Bot instance. Register any bot-dependent commands (e.g. music) before calling Run.
func NewBot(cfg *config.Config, storage *storage.Storage) *Bot {
	return &Bot{
		cfg:       cfg,
		storage:   storage,
		slashCmds: make(map[string][]*discordgo.ApplicationCommand),
		players:   make(map[string]*player.Player),
	}
}

// Run starts the Discord session and runs until ctx is done.
func (b *Bot) Run(ctx context.Context) error {
	return b.run(ctx, b.cfg.DiscordToken)
}

// StartBot is a convenience that creates a bot and runs it. Use NewBot + RegisterCommand + Run when you need to register bot-dependent commands (e.g. music).
func StartBot(ctx context.Context, cfg *config.Config, storage *storage.Storage) error {
	b := NewBot(cfg, storage)
	return b.Run(ctx)
}

// run starts the Discord bot
func (b *Bot) run(ctx context.Context, token string) error {
	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}

	b.dg = dg

	b.configureIntents()
	dg.AddHandler(b.onReady)
	dg.AddHandler(b.onMessageCreate)
	dg.AddHandler(b.onMessageReactionAdd)
	dg.AddHandler(b.onInteractionCreate)
	dg.AddHandler(b.onGuildCreate)

	if err := dg.Open(); err != nil {
		return fmt.Errorf("failed to open Discord session: %w", err)
	}
	defer dg.Close()

	go func() {
		for evt := range SystemEvents() {
			switch evt.Type {
			case SystemEventRefreshCommands:
				go b.handleRefreshCommands(evt)
			}
		}
	}()

	<-ctx.Done()
	log.Println("[INFO] ❎ Shutdown signal received. Cleaning up...")
	return nil
}

// configureIntents configures the Discord intents
func (b *Bot) configureIntents() {
	b.dg.Identify.Intents = discordgo.IntentsAll
}

// onMessageCreate is called when a message is created
func (b *Bot) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	mentioned := false
	for _, user := range m.Mentions {
		if user.ID == s.State.User.ID {
			mentioned = true
			break
		}
	}
	if !mentioned {
		return
	}

	msgCtx := &command.MessageContext{Session: s, Event: m, Storage: b.storage, Config: b.cfg}
	inv := &cmd.Invocation{Data: msgCtx}
	for _, c := range cmd.DefaultRegistry.GetAll() {
		if err := c.Run(context.Background(), inv); err != nil {
			log.Println("[ERR] Error running command:", err)
			MessageEmbed(s, m.ChannelID, &discordgo.MessageEmbed{
				Description: fmt.Sprintf("Error running command: %v", err),
			})
		}
	}
}

// onReady is called when the bot is ready
func (b *Bot) onReady(s *discordgo.Session, r *discordgo.Ready) {
	botInfo, err := s.User("@me")
	if err != nil {
		log.Println("[WARN] Error retrieving bot user:", err)
		return
	}

	// Leave any blacklisted guilds on startup
	for _, g := range r.Guilds {
		if b.isGuildBlacklisted(g.ID) {
			log.Printf("[INFO] Leaving blacklisted guild: %s (%s)", g.ID, g.Name)
			if err := s.GuildLeave(g.ID); err != nil {
				log.Printf("[ERR] Failed to leave guild %s: %v", g.ID, err)
			}
			continue
		}

		if b.cfg.InitSlashCommands {
			if err := b.registerCommands(g.ID); err != nil {
				log.Println("[ERR] Error registering slash commands for guild", g.ID, ":", err)
			}
		} else {
			log.Println("[INFO] Registering slash commands skipped")
		}
	}

	if err := docs.UpdateReadme(cmd.DefaultRegistry, config.CategoryWeights); err != nil {
		log.Println("[ERR] Failed to update README:", err)
	}

	log.Printf("[INFO] ✅ Discord bot %v is running.", botInfo.Username)
}

// onGuildCreate is called when a guild is created
func (b *Bot) onGuildCreate(s *discordgo.Session, g *discordgo.GuildCreate) {
	log.Printf("[INFO] Bot added to guild: %s (%s)", g.Guild.ID, g.Guild.Name)

	if b.isGuildBlacklisted(g.Guild.ID) {
		log.Printf("[INFO] Leaving blacklisted guild: %s (%s)", g.Guild.ID, g.Guild.Name)
		if err := s.GuildLeave(g.Guild.ID); err != nil {
			log.Printf("[ERR] Failed to leave guild %s: %v", g.Guild.ID, err)
		}
		return
	}

	if err := b.registerCommands(g.Guild.ID); err != nil {
		log.Printf("[ERR] Failed to register commands for new guild %s: %v", g.Guild.ID, err)
	}
}

// onMessageReactionAdd is called when a reaction is added
func (b *Bot) onMessageReactionAdd(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
	reactionCtx := &command.MessageReactionContext{Session: s, Event: r, Storage: b.storage, Config: b.cfg, Logger: DefaultLogger}
	inv := &cmd.Invocation{Data: reactionCtx}
	for _, c := range cmd.DefaultRegistry.GetAll() {
		if _, ok := cmd.Root(c).(command.ReactionProvider); ok {
			if err := c.Run(context.Background(), inv); err != nil {
				log.Println("[ERR] Error running reaction command:", err)
				MessageEmbed(s, r.ChannelID, &discordgo.MessageEmbed{
					Description: fmt.Sprintf("Error running reaction command: %v", err),
				})
			}
		}
	}
}

// onInteractionCreate is called when an interaction is created
func (b *Bot) onInteractionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		cmdName := i.ApplicationCommandData().Name
		c := cmd.DefaultRegistry.Get(cmdName)
		if c == nil {
			log.Printf("[WARN] Unknown command: %s\n", cmdName)
			return
		}

		switch i.ApplicationCommandData().CommandType {
		case discordgo.MessageApplicationCommand:
			appCtx := &command.MessageApplicationCommandContext{
				Session: s, Event: i, Storage: b.storage, Target: i.Message, Config: b.cfg,
				Responder: DefaultResponder, Logger: DefaultLogger,
			}
			inv := &cmd.Invocation{Data: appCtx}
			if err := c.Run(context.Background(), inv); err != nil {
				log.Println("[ERR] Error running context menu command:", err)
				RespondEmbedEphemeral(s, i, &discordgo.MessageEmbed{Description: fmt.Sprintf("Error running context menu command: %v", err)})
			}
		case discordgo.ChatApplicationCommand:
			slashCtx := &command.SlashInteractionContext{
				Session: s, Event: i, Storage: b.storage, Config: b.cfg, Responder: DefaultResponder, Logger: DefaultLogger,
			}
			inv := &cmd.Invocation{Data: slashCtx}
			if err := c.Run(context.Background(), inv); err != nil {
				log.Println("[ERR] Error running slash command:", err)
				RespondEmbedEphemeral(s, i, &discordgo.MessageEmbed{Description: fmt.Sprintf("Error running slash command: %v", err)})
			}
		}

	case discordgo.InteractionMessageComponent:
		customID := i.MessageComponentData().CustomID
		log.Printf("[DEBUG] Processing component interaction: %s\n", customID)

		var matched cmd.Command
		for _, c := range cmd.DefaultRegistry.GetAll() {
			if strings.HasPrefix(customID, c.Name()) || strings.HasPrefix(customID, c.Name()+":") || strings.HasPrefix(customID, c.Name()+"_") {
				matched = c
				log.Printf("[DEBUG] Found matching command: %s\n", c.Name())
				break
			}
		}

		if matched != nil {
			log.Printf("[DEBUG] Matched command type: %T", matched)
			root := cmd.Root(matched)
			compHandler, ok := root.(command.ComponentInteractionHandler)
			log.Printf("[DEBUG] ComponentInteractionHandler? %v", ok)
			if ok {
				log.Printf("[DEBUG] Command %s implements ComponentHandler\n", matched.Name())
				log.Printf("[DEBUG] About to call Component() method...\n")
				compCtx := &command.ComponentInteractionContext{
					Session: s, Event: i, Storage: b.storage, Config: b.cfg, Responder: DefaultResponder, Logger: DefaultLogger,
				}
				err := compHandler.Component(compCtx)
				if err != nil {
					log.Printf("[ERR] Error running component command %s: %v\n", matched.Name(), err)
					RespondEmbedEphemeral(s, i, &discordgo.MessageEmbed{Description: fmt.Sprintf("Error running component command: %v", err)})
				}
				log.Printf("[DEBUG] Component() method completed: %s\n", matched.Name())
			} else {
				log.Printf("[WARN] Command %s does not implement ComponentHandler interface\n", matched.Name())
				log.Printf("[DEBUG] Command type: %T\n", matched)
			}
		} else {
			log.Printf("[WARN] No matching component for customID: %s\n", customID)
		}

	default:
		log.Printf("[DEBUG] Unknown interaction type: %d\n", i.Type)
	}
}

// registerCommands registers slash commands
func (b *Bot) registerCommands(guildID string) error {
	appID := b.dg.State.User.ID
	if appID == "" {
		user, err := b.dg.User("@me")
		if err != nil {
			return err
		}
		appID = user.ID
	}

	existing, _ := b.dg.ApplicationCommands(appID, guildID)
	localHashes := loadGuildCommandHashes(guildID)

	var wanted []*discordgo.ApplicationCommand
	wantedHashes := make(map[string]string)
	for _, c := range cmd.DefaultRegistry.GetAll() {
		if def := normalizeDefinition(c); def != nil {
			wanted = append(wanted, def)
			wantedHashes[def.Name] = hashCommand(def)
		}
	}

	// Delete obsolete
	for _, old := range existing {
		if _, ok := wantedHashes[old.Name]; !ok {
			log.Printf("[INFO] [%s] Deleting obsolete command: %s", guildID, old.Name)
			if err := b.dg.ApplicationCommandDelete(appID, guildID, old.ID); err != nil {
				log.Printf("[ERR] [%s] Failed to delete %s: %v", guildID, old.Name, err)
			}
			delete(localHashes, old.Name)
		}
	}

	// Create or update changed commands
	var changed []*discordgo.ApplicationCommand
	for _, cmd := range wanted {
		newHash := wantedHashes[cmd.Name]
		if localHashes[cmd.Name] != newHash {
			changed = append(changed, cmd)
		}
	}

	if len(changed) > 0 {
		log.Printf("[INFO] [%s] %d commands changed — updating with rate limit...", guildID, len(changed))
		registerCommandsWithRateLimit(b, guildID, changed)
		for _, c := range changed {
			localHashes[c.Name] = wantedHashes[c.Name]
		}
	}

	saveGuildCommandHashes(guildID, localHashes)
	return nil
}

func (b *Bot) isGuildBlacklisted(guildID string) bool {
	return slices.Contains(b.cfg.DiscordGuildBlacklist, guildID)
}

// normalizeDefinition normalizes a command definition (uses root so wrapped commands still expose providers)
func normalizeDefinition(c cmd.Command) *discordgo.ApplicationCommand {
	root := cmd.Root(c)
	if slash, ok := root.(command.SlashProvider); ok {
		if def := slash.SlashDefinition(); def != nil {
			if def.Type == 0 {
				def.Type = discordgo.ChatApplicationCommand
			}
			return def
		}
	}
	if menu, ok := root.(command.ContextMenuProvider); ok {
		if def := menu.ContextDefinition(); def != nil {
			if def.Type == 0 {
				def.Type = discordgo.MessageApplicationCommand
			}
			return def
		}
	}
	return nil
}

// registerCommandsWithRateLimit registers commands with a rate limit
func registerCommandsWithRateLimit(b *Bot, guildID string, cmds []*discordgo.ApplicationCommand) {
	rateLimit := time.Second / 40
	ticker := time.NewTicker(rateLimit)
	defer ticker.Stop()

	var wg sync.WaitGroup

	for _, job := range cmds {
		wg.Add(1)

		go func(cmd *discordgo.ApplicationCommand) {
			defer wg.Done()
			<-ticker.C

			_, err := b.dg.ApplicationCommandCreate(b.dg.State.User.ID, guildID, cmd)
			if err != nil {
				log.Printf("[ERR] Can't create command %s: %v", cmd.Name, err)
			} else {
				log.Printf("[DONE] Command created: %s", cmd.Name)
			}
		}(job)
	}

	wg.Wait()
}

func (b *Bot) handleRefreshCommands(evt SystemEvent) {
	appID := b.dg.State.User.ID
	if appID == "" {
		user, err := b.dg.User("@me")
		if err != nil {
			log.Printf("[ERR][%s] Failed to fetch self: %v", evt.GuildID, err)
			return
		}
		appID = user.ID
	}

	// Fetch existing commands for the guild
	existing, _ := b.dg.ApplicationCommands(appID, evt.GuildID)

	// If guild is blacklisted, forcibly delete all commands
	if b.isGuildBlacklisted(evt.GuildID) {
		log.Printf("[BLACKLIST][%s] Guild is blacklisted — removing all commands", evt.GuildID)
		for _, cmd := range existing {
			if err := b.dg.ApplicationCommandDelete(appID, evt.GuildID, cmd.ID); err != nil {
				log.Printf("[ERR][%s] Failed to delete command %s: %v", evt.GuildID, cmd.Name, err)
			} else {
				log.Printf("[DONE][%s] Deleted command %s", evt.GuildID, cmd.Name)
			}
		}
		return
	}

	// Handle group-specific enable/disable
	if strings.HasPrefix(evt.Target, "group:") {
		group := strings.TrimPrefix(evt.Target, "group:")
		disabledGroups, _ := b.storage.GetDisabledGroups(evt.GuildID)
		disabledMap := make(map[string]bool)
		for _, g := range disabledGroups {
			disabledMap[g] = true
		}

		for _, c := range cmd.DefaultRegistry.GetAll() {
			meta, ok := cmd.Root(c).(command.DiscordMeta)
			if !ok || meta.Group() != group {
				continue
			}
			found := false
			for _, ex := range existing {
				if ex.Name == c.Name() {
					found = true
					if disabledMap[group] {
						log.Printf("[INFO][%s] Deleting disabled command %s", evt.GuildID, c.Name())
						_ = b.dg.ApplicationCommandDelete(appID, evt.GuildID, ex.ID)
					}
					break
				}
			}
			if !found && !disabledMap[group] {
				if def := normalizeDefinition(c); def != nil {
					log.Printf("[INFO][%s] Registering enabled command %s", evt.GuildID, c.Name())
					_, _ = b.dg.ApplicationCommandCreate(appID, evt.GuildID, def)
				}
			}
		}
		return
	}

	// Refresh all commands
	if strings.ToLower(evt.Target) == "all" || evt.Target == "" {
		_ = b.registerCommands(evt.GuildID)
		return
	}

	// Refresh single command by name
	for _, c := range cmd.DefaultRegistry.GetAll() {
		if strings.EqualFold(c.Name(), evt.Target) {
			if def := normalizeDefinition(c); def != nil {
				_, _ = b.dg.ApplicationCommandCreate(appID, evt.GuildID, def)
			}
			return
		}
	}
}
