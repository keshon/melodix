// FILE: melodix/internal/discord/bot.go
package discord

import (
	"context"
	"fmt"
	"log"
	"slices"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/commandkit"
	"github.com/keshon/melodix/internal/command"
	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/discord/command_logger"
	"github.com/keshon/melodix/internal/discord/command_manager"
	"github.com/keshon/melodix/internal/discord/voice"
	"github.com/keshon/melodix/internal/readme"
	"github.com/keshon/melodix/internal/storage"
)

// Bot is the Discord bot. Lifecycle is managed by Run/run; handlers are wired in run.
type Bot struct {
	cfg     *config.Config
	storage *storage.Storage

	mu    sync.RWMutex
	dg    *discordgo.Session
	voice *voice.Service

	cmdManager *command_manager.Manager
	cmdLogger  *command_logger.CommandLogger // recreated each session alongside dg

	// invokeCtx is cancelled when the Discord session ends. Passed to slash
	// commands so long-running handlers can respect shutdown.
	invokeCtx context.Context

	// once ensures one-time background services (readme, purge, …) are not
	// re-launched on subsequent reconnects.
	once sync.Once
}

// NewBot creates a Bot. Register any bot-dependent commands before calling Run.
func NewBot(cfg *config.Config, storage *storage.Storage) *Bot {
	return &Bot{
		cfg:     cfg,
		storage: storage,
	}
}

// StartBot is a convenience constructor + runner.
// Use NewBot + Run directly when you need to register commands before starting.
func StartBot(ctx context.Context, cfg *config.Config, storage *storage.Storage) error {
	return NewBot(cfg, storage).Run(ctx)
}

// Run starts the bot, restarting the session on disconnect until ctx is cancelled.
func (b *Bot) Run(ctx context.Context) error {
	for {
		if err := b.run(ctx); err != nil {
			log.Println("[ERR] Bot session ended:", err)
		}
		select {
		case <-ctx.Done():
			return nil
		default:
			log.Println("[WARN] Restarting Discord session in 5 seconds...")
			time.Sleep(5 * time.Second)
		}
	}
}

// run opens one Discord session and blocks until ctx is cancelled or the connection is lost.
func (b *Bot) run(ctx context.Context) error {
	dg, err := discordgo.New("Bot " + b.cfg.DiscordToken)
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	dg.LogLevel = discordgo.LogInformational

	// All session-scoped services are recreated here so they always hold a
	// reference to the current *discordgo.Session, not a stale one from a
	// previous connect cycle.
	b.mu.Lock()
	b.dg = dg
	b.voice = voice.New(func() *discordgo.Session {
		b.mu.RLock()
		s := b.dg
		b.mu.RUnlock()
		return s
	}, b.cfg, b.storage)

	b.cmdLogger = command_logger.NewCommandLogger(dg, b.storage)
	b.cmdManager = command_manager.NewManager(dg, b.storage, commandkit.DefaultRegistry)

	b.mu.Unlock()

	sessionCtx, cancelSession := context.WithCancel(ctx)
	defer cancelSession()

	b.mu.Lock()
	b.invokeCtx = sessionCtx
	b.mu.Unlock()

	disconnected := make(chan struct{})
	var disconnectOnce sync.Once
	notifyDisconnect := func() {
		disconnectOnce.Do(func() {
			log.Println("[WARN] WebSocket disconnected — will restart session")
			close(disconnected)
		})
	}

	dg.AddHandler(func(_ *discordgo.Session, _ *discordgo.Disconnect) {
		select {
		case <-ctx.Done():
			return
		default:
			notifyDisconnect()
		}
	})

	b.configureIntents()
	dg.AddHandler(b.onReady)
	dg.AddHandler(b.onGuildCreate)
	dg.AddHandler(b.onMessageCreate)
	dg.AddHandler(b.onMessageReactionAdd)
	dg.AddHandler(b.onInteractionCreate)

	if err := dg.Open(); err != nil {
		return fmt.Errorf("failed to open Discord session: %w", err)
	}
	defer func() {
		log.Println("[INFO] Closing Discord session...")
		dg.Close()
	}()

	go b.forwardSystemEvents(sessionCtx)
	go b.monitorConnection(sessionCtx, dg, notifyDisconnect)

	select {
	case <-ctx.Done():
		log.Println("[INFO] ❎ Shutdown signal received. Cleaning up...")
		b.stopAllPlayers()
		return nil
	case <-disconnected:
		return fmt.Errorf("websocket disconnected")
	}
}

// --- Background goroutines ---

// forwardSystemEvents listens for bot-level system events and routes them to the right handler.
func (b *Bot) forwardSystemEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-SystemEvents():
			if !ok {
				return
			}
			if evt.Type == SystemEventRefreshCommands {
				go b.cmdManager.RefreshAll()
			}
		}
	}
}

// monitorConnection probes the Discord API periodically and triggers a reconnect
// after 3 consecutive failures.
func (b *Bot) monitorConnection(ctx context.Context, dg *discordgo.Session, notifyDisconnect func()) {
	// Give the connection time to stabilise before starting health checks.
	select {
	case <-ctx.Done():
		return
	case <-time.After(15 * time.Second):
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	fails := 0

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if dg.HeartbeatLatency() < 0 {
				continue
			}
			if _, err := dg.User("@me"); err != nil {
				fails++
				log.Printf("[WARN] API probe failed (%d/3): %v", fails, err)
				if fails >= 3 {
					log.Println("[WARN] 3 consecutive API probe failures — reconnecting")
					notifyDisconnect()
					return
				}
			} else {
				fails = 0
			}
		}
	}
}

// --- Discord event handlers ---

// onReady fires on every successful connect/reconnect.
func (b *Bot) onReady(s *discordgo.Session, r *discordgo.Ready) {
	botInfo, err := s.User("@me")
	if err != nil {
		log.Println("[WARN] Error retrieving bot user:", err)
		return
	}

	for _, g := range r.Guilds {
		if b.isGuildBlacklisted(g.ID) {
			log.Printf("[INFO] Leaving blacklisted guild: %s", g.ID)
			if err := s.GuildLeave(g.ID); err != nil {
				log.Printf("[ERR] Failed to leave guild %s: %v", g.ID, err)
			}
			continue
		}
		if b.cfg.InitSlashCommands {
			if err := b.cmdManager.RegisterCommands(g.ID); err != nil {
				log.Printf("[ERR] Error registering commands for guild %s: %v", g.ID, err)
			}
		}
	}

	b.once.Do(func() {
		log.Println("[INFO] Starting background services...")
		if err := readme.UpdateReadme(commandkit.DefaultRegistry, config.CategoryWeights); err != nil {
			log.Println("[ERR] Failed to update README:", err)
		}
	})

	log.Printf("[INFO] ✅ Discord bot %v is ready.", botInfo.Username)
}

// onGuildCreate fires when the bot joins a new guild.
func (b *Bot) onGuildCreate(s *discordgo.Session, g *discordgo.GuildCreate) {
	log.Printf("[INFO] Bot added to guild: %s (%s)", g.Guild.ID, g.Guild.Name)
	if b.isGuildBlacklisted(g.Guild.ID) {
		log.Printf("[INFO] Leaving blacklisted guild: %s", g.Guild.ID)
		if err := s.GuildLeave(g.Guild.ID); err != nil {
			log.Printf("[ERR] Failed to leave guild %s: %v", g.Guild.ID, err)
		}
		return
	}
	if b.cfg.InitSlashCommands {
		if err := b.cmdManager.RegisterCommands(g.Guild.ID); err != nil {
			log.Printf("[ERR] Failed to register commands for guild %s: %v", g.Guild.ID, err)
		}
	}
}

// onMessageCreate handles @mention messages directed at the bot.
func (b *Bot) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	mentioned := false
	for _, u := range m.Mentions {
		if u.ID == s.State.User.ID {
			mentioned = true
			break
		}
	}
	if !mentioned {
		return
	}

	inv := &commandkit.Invocation{Data: &command.MessageContext{
		Session: s, Event: m, Storage: b.storage, Config: b.cfg,
	}}
	for _, c := range commandkit.DefaultRegistry.GetAll() {
		if err := c.Run(context.Background(), inv); err != nil {
			log.Println("[ERR] Error running message command:", err)
			MessageEmbed(s, m.ChannelID, &discordgo.MessageEmbed{
				Description: fmt.Sprintf("Error: %v", err),
			})
		}
	}
}

// onMessageReactionAdd handles reaction events for commands that use reactions.
func (b *Bot) onMessageReactionAdd(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
	b.mu.RLock()
	logger := b.cmdLogger
	b.mu.RUnlock()

	inv := &commandkit.Invocation{Data: &command.MessageReactionContext{
		Session: s, Event: r, Storage: b.storage, Config: b.cfg, Logger: logger,
	}}
	for _, c := range commandkit.DefaultRegistry.GetAll() {
		if _, ok := commandkit.Root(c).(command.ReactionProvider); !ok {
			continue
		}
		if err := c.Run(context.Background(), inv); err != nil {
			log.Println("[ERR] Error running reaction command:", err)
			MessageEmbed(s, r.ChannelID, &discordgo.MessageEmbed{
				Description: fmt.Sprintf("Error: %v", err),
			})
		}
	}
}

// onInteractionCreate dispatches slash commands, context menu commands, and component interactions.
func (b *Bot) onInteractionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		b.handleApplicationCommand(s, i)
	case discordgo.InteractionMessageComponent:
		b.handleComponentInteraction(s, i)
	default:
		log.Printf("[DEBUG] Unhandled interaction type: %d", i.Type)
	}
}

func (b *Bot) handleApplicationCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	name := i.ApplicationCommandData().Name
	c := commandkit.DefaultRegistry.Get(name)
	if c == nil {
		log.Printf("[WARN] Unknown command: %s", name)
		return
	}

	b.mu.RLock()
	cmdCtx := b.invokeCtx
	logger := b.cmdLogger
	b.mu.RUnlock()

	if cmdCtx == nil {
		cmdCtx = context.Background()
	}

	var inv *commandkit.Invocation
	switch i.ApplicationCommandData().CommandType {
	case discordgo.MessageApplicationCommand:
		inv = &commandkit.Invocation{Data: &command.MessageApplicationCommandContext{
			Session:   s,
			Event:     i,
			Storage:   b.storage,
			Target:    i.Message,
			Config:    b.cfg,
			Responder: DefaultResponder,
			Logger:    logger,
		}}
	case discordgo.ChatApplicationCommand:
		inv = &commandkit.Invocation{Data: &command.SlashInteractionContext{
			Ctx:       cmdCtx,
			Session:   s,
			Event:     i,
			Storage:   b.storage,
			Config:    b.cfg,
			Responder: DefaultResponder,
			Logger:    logger,
		}}
	default:
		return
	}

	if err := c.Run(context.Background(), inv); err != nil {
		log.Printf("[ERR] Error running command %s: %v", name, err)
		RespondEmbedEphemeral(s, i, &discordgo.MessageEmbed{
			Description: fmt.Sprintf("Error running command: %v", err),
		})
	}
}

func (b *Bot) handleComponentInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID
	log.Printf("[DEBUG] Component interaction: %s", customID)

	var matched commandkit.Command
	for _, c := range commandkit.DefaultRegistry.GetAll() {
		if matchesComponentID(customID, c.Name()) {
			matched = c
			break
		}
	}
	if matched == nil {
		log.Printf("[WARN] No component handler for customID: %s", customID)
		return
	}

	handler, ok := commandkit.Root(matched).(command.ComponentInteractionHandler)
	if !ok {
		log.Printf("[WARN] Command %s does not implement ComponentInteractionHandler", matched.Name())
		return
	}

	b.mu.RLock()
	logger := b.cmdLogger
	b.mu.RUnlock()

	err := handler.Component(&command.ComponentInteractionContext{
		Session:   s,
		Event:     i,
		Storage:   b.storage,
		Config:    b.cfg,
		Responder: DefaultResponder,
		Logger:    logger,
	})
	if err != nil {
		log.Printf("[ERR] Error in component handler %s: %v", matched.Name(), err)
		RespondEmbedEphemeral(s, i, &discordgo.MessageEmbed{
			Description: fmt.Sprintf("Error: %v", err),
		})
	}
}

// --- Utility ---

// matchesComponentID reports whether a component customID belongs to a command.
// CustomIDs follow the convention "commandName", "commandName:...", or "commandName_...".
func matchesComponentID(customID, commandName string) bool {
	if customID == commandName {
		return true
	}
	if len(customID) > len(commandName) {
		sep := customID[len(commandName)]
		return (sep == ':' || sep == '_') && customID[:len(commandName)] == commandName
	}
	return false
}

// isGuildBlacklisted reports whether a guild is on the blacklist.
func (b *Bot) isGuildBlacklisted(guildID string) bool {
	return slices.Contains(b.cfg.DiscordGuildBlacklist, guildID)
}

// stopAllPlayers stops playback and disconnects voice in all guilds. Call on shutdown.
func (b *Bot) stopAllPlayers() {
	if b.voice != nil {
		b.voice.StopAllPlayers()
	}
	log.Println("[INFO] All players stopped")
}

// configureIntents sets the gateway intents for the session.
func (b *Bot) configureIntents() {
	b.dg.Identify.Intents = discordgo.IntentsAll
}
