package discord

import (
	"context"
	"fmt"
	"log"

	"github.com/keshon/melodix/internal/storage"
	"github.com/keshon/melodix/pkg/commandkit"
	"github.com/keshon/melodix/pkg/music/player"

	"slices"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/internal/command"
	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/docs"

	"github.com/keshon/melodix/pkg/music/source_resolver"
)

// guildMusicStatus holds the channel and message ID for a guild's "now playing" message,
// so we can edit it for later updates (works beyond interaction token expiry).
type guildMusicStatus struct {
	ChannelID string
	MessageID string
}

// Bot is the Discord bot. Lifecycle is managed by Run/run; handlers are wired in run.
type Bot struct {
	dg                 *discordgo.Session
	storage            *storage.Storage
	slashCmds          map[string][]*discordgo.ApplicationCommand
	cfg                *config.Config
	mu                 sync.RWMutex
	players            map[string]*player.Player
	sourceResolver     *source_resolver.SourceResolver
	guildMusicStatus   map[string]guildMusicStatus
	guildMusicStatusMu sync.RWMutex

	// once ensures one-time background services (purge, shortlink) are not
	// re-launched on subsequent reconnects.
	once sync.Once
}

// NewBot creates a Bot. Register any bot-dependent commands before calling Run.
func NewBot(cfg *config.Config, storage *storage.Storage) *Bot {
	return &Bot{
		cfg:              cfg,
		storage:          storage,
		slashCmds:        make(map[string][]*discordgo.ApplicationCommand),
		players:          make(map[string]*player.Player),
		guildMusicStatus: make(map[string]guildMusicStatus),
	}
}

// StartBot is a convenience constructor + runner.
// Use NewBot + Run directly when you need to register bot-dependent commands first.
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

	b.mu.Lock()
	b.dg = dg
	b.mu.Unlock()

	// disconnected is closed once — multiple concurrent signals (our handler + discordgo
	// internal reconnect attempts) collapse into a single restart.
	disconnected := make(chan struct{})
	var disconnectOnce sync.Once
	notifyDisconnect := func() {
		disconnectOnce.Do(func() {
			log.Println("[WARN] WebSocket disconnected — will restart session")
			close(disconnected)
		})
	}

	dg.AddHandler(func(_ *discordgo.Session, _ *discordgo.Disconnect) { notifyDisconnect() })

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

	sessionCtx, cancelSession := context.WithCancel(ctx)
	defer cancelSession()

	// Forward system events (e.g. command refresh) to the handler.
	go func() {
		for {
			select {
			case <-sessionCtx.Done():
				return
			case evt, ok := <-SystemEvents():
				if !ok {
					return
				}
				if evt.Type == SystemEventRefreshCommands {
					go b.handleRefreshCommands(evt)
				}
			}
		}
	}()

	// Connection health monitor: active API probe every 30s.
	// HeartbeatLatency alone is unreliable after system sleep — the TCP connection
	// may appear alive while Discord is actually unreachable.
	go func() {
		select {
		case <-sessionCtx.Done():
			return
		case <-time.After(15 * time.Second): // let the session settle first
		}

		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		fails := 0

		for {
			select {
			case <-sessionCtx.Done():
				return
			case <-ticker.C:
				// Negative latency is normal during discordgo's internal reconnect cycle —
				// it resets the heartbeat timer and the next ACK appears to arrive "before"
				// the send. Skip the probe this tick and let discordgo handle it.
				lat := dg.HeartbeatLatency()
				if lat < 0 {
					log.Printf("[DEBUG] Heartbeat latency negative (%v), skipping probe this tick", lat)
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
					if fails > 0 {
						log.Printf("[INFO] API probe recovered after %d failure(s)", fails)
					}
					fails = 0
					log.Printf("[DEBUG] Heartbeat latency: %v", lat)
				}
			}
		}
	}()

	select {
	case <-ctx.Done():
		log.Println("[INFO] ❎ Shutdown signal received. Cleaning up...")
		b.stopAllPlayers()
		return nil
	case <-disconnected:
		return fmt.Errorf("websocket disconnected")
	}
}

// stopAllPlayers stops playback and disconnects voice for all guilds. Call on shutdown.
func (b *Bot) stopAllPlayers() {
	b.mu.Lock()
	players := make(map[string]*player.Player, len(b.players))
	for k, v := range b.players {
		players[k] = v
	}
	b.mu.Unlock()

	for _, p := range players {
		_ = p.Stop(true)
	}

	b.mu.Lock()
	b.players = make(map[string]*player.Player)
	b.mu.Unlock()
	log.Println("[INFO] All players stopped")
}

func (b *Bot) configureIntents() {
	b.dg.Identify.Intents = discordgo.IntentsAll
}

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
			if err := b.registerCommands(g.ID); err != nil {
				log.Printf("[ERR] Error registering slash commands for guild %s: %v", g.ID, err)
			}
		}
	}

	// Background services start once across all reconnects.
	b.once.Do(func() {
		log.Println("[INFO] Starting background services...")
		if err := docs.UpdateReadme(commandkit.DefaultRegistry, config.CategoryWeights); err != nil {
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
	if err := b.registerCommands(g.Guild.ID); err != nil {
		log.Printf("[ERR] Failed to register commands for guild %s: %v", g.Guild.ID, err)
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

	inv := &commandkit.Invocation{Data: &command.MessageContext{Session: s, Event: m, Storage: b.storage, Config: b.cfg}}
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
	inv := &commandkit.Invocation{Data: &command.MessageReactionContext{
		Session: s, Event: r, Storage: b.storage, Config: b.cfg, Logger: DefaultLogger,
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

	var inv *commandkit.Invocation
	switch i.ApplicationCommandData().CommandType {
	case discordgo.MessageApplicationCommand:
		inv = &commandkit.Invocation{Data: &command.MessageApplicationCommandContext{
			Session: s, Event: i, Storage: b.storage, Target: i.Message,
			Config: b.cfg, Responder: DefaultResponder, Logger: DefaultLogger,
		}}
	case discordgo.ChatApplicationCommand:
		inv = &commandkit.Invocation{Data: &command.SlashInteractionContext{
			Session: s, Event: i, Storage: b.storage,
			Config: b.cfg, Responder: DefaultResponder, Logger: DefaultLogger,
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

	err := handler.Component(&command.ComponentInteractionContext{
		Session: s, Event: i, Storage: b.storage,
		Config: b.cfg, Responder: DefaultResponder, Logger: DefaultLogger,
	})
	if err != nil {
		log.Printf("[ERR] Error in component handler %s: %v", matched.Name(), err)
		RespondEmbedEphemeral(s, i, &discordgo.MessageEmbed{
			Description: fmt.Sprintf("Error: %v", err),
		})
	}
}

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

func (b *Bot) isGuildBlacklisted(guildID string) bool {
	return slices.Contains(b.cfg.DiscordGuildBlacklist, guildID)
}
