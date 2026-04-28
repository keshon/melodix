package discord

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/commandkit"
	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/discord/commandlogger"
	"github.com/keshon/melodix/internal/discord/commandsync"
	"github.com/keshon/melodix/internal/discord/execguard"
	"github.com/keshon/melodix/internal/discord/systemevents"
	"github.com/keshon/melodix/internal/discord/voice"
	"github.com/keshon/melodix/internal/discord/watchdog"
	"github.com/keshon/melodix/internal/storage"
	"github.com/rs/zerolog"
)

// NewBot creates a Bot. Register any bot-dependent commands before calling Run.
func NewBot(cfg *config.Config, storage *storage.Storage, log zerolog.Logger) *Bot {
	b := &Bot{
		cfg:       cfg,
		storage:   storage,
		log:       log,
		slashCmds: make(map[string][]*discordgo.ApplicationCommand),
		systemBus: systemevents.New(32),
	}
	// Voice service must outlive a single Discord session so playback/queues survive reconnects.
	b.voice = voice.New(func() *discordgo.Session {
		b.mu.RLock()
		s := b.dg
		b.mu.RUnlock()
		return s
	}, cfg, storage, log)
	b.sessionCtx.Store(&sessionCtxHolder{ctx: context.Background()})
	b.cmdGuard.Store(&cmdGuardHolder{g: disabledGuard})
	return b
}

// RunSession opens one Discord session and blocks until ctx is cancelled or the API probe
// decides the session is unhealthy (transient gateway reconnects do not exit this function).
func (b *Bot) RunSession(ctx context.Context) error {
	// --- Discord session bootstrap (discordgo.Session) ---
	dg, err := discordgo.New("Bot " + b.cfg.DiscordToken)
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	// So voice connection inherits LogInformational and we see OP2/OP4/DAVE handshake in logs.
	dg.LogLevel = discordgo.LogInformational

	// --- Core services wiring (voice, cmd manager, cmd logger) ---
	b.mu.Lock()
	b.dg = dg
	b.cmdLogger = commandlogger.New(dg, b.storage, b.log)
	b.cmdManager = commandsync.NewSyncer(dg, commandkit.DefaultRegistry, b.log)
	attachDiscordgoLogger(b.log)

	b.mu.Unlock()

	// --- Guardrails: command timeout + global parallelism limiter ---
	b.cmdGuard.Store(&cmdGuardHolder{g: execguard.New(b.cfg.CommandTimeout, b.cfg.CommandParallelism)})

	// --- Health tracking: WS activity + ready marker (used by watchdogs) ---
	tracker := watchdog.NewTracker()

	// disconnected is closed once when we decide the session is unusable (see API probe below).
	// We intentionally do not hook discordgo.Disconnect: the library reconnects the gateway on Op7
	// and similar events; treating every Disconnect as fatal caused dg.Close() to race with that
	// reconnect and wiped in-memory voice/queue state.
	disconnected := make(chan struct{})
	var restartOnce sync.Once
	var unhealthyMu sync.Mutex
	var unhealthyCount int
	var unhealthyWindowStart time.Time

	invalidateSinks := func() {
		if b.voice != nil {
			b.voice.InvalidateAllSinks()
		}
	}

	notifyUnhealthy := func() {
		mode := b.cfg.DiscordUnhealthyMode
		switch mode {
		case "ignore":
			return
		case "restart-voice":
			invalidateSinks()
			return
		case "restart-session", "":
			// fallthrough to restart logic
		default:
			b.log.Warn().Str("mode", mode).Msg("discord_unhealthy_mode_unknown")
		}

		// mode=restart-session: optionally ignore first N signals within a window (still invalidating sinks).
		grace := b.cfg.DiscordUnhealthyGrace
		if grace < 0 {
			grace = 0
		}
		window := b.cfg.DiscordUnhealthyWindow
		if window <= 0 {
			window = time.Minute
		}

		shouldRestart := true
		if grace > 0 {
			now := time.Now()
			unhealthyMu.Lock()
			if unhealthyWindowStart.IsZero() || now.Sub(unhealthyWindowStart) > window {
				unhealthyWindowStart = now
				unhealthyCount = 0
			}
			unhealthyCount++
			if unhealthyCount <= grace {
				shouldRestart = false
			}
			unhealthyMu.Unlock()
		}

		if !shouldRestart {
			invalidateSinks()
			return
		}

		restartOnce.Do(func() {
			b.log.Warn().Msg("discord_session_unhealthy")
			// Soft-restart path: keep players/queues, but invalidate transport so they recover fast.
			invalidateSinks()
			close(disconnected)
		})
	}

	// --- Discord intents + event handlers wiring ---
	b.configureIntents()
	dg.AddHandler(func(s *discordgo.Session, e *discordgo.Event) {
		_ = s
		_ = e
		tracker.MarkWSNow()
	})
	dg.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		tracker.MarkReadyNow()
		b.onReady(s, r)
	})
	dg.AddHandler(b.onGuildCreate)
	dg.AddHandler(b.onMessageCreate)
	dg.AddHandler(b.onMessageReactionAdd)
	dg.AddHandler(b.onInteractionCreate)

	// --- Session-scoped context (cancels on session restart/shutdown) ---
	sessionCtx, cancelSession := context.WithCancel(ctx)
	b.sessionCtx.Store(&sessionCtxHolder{ctx: sessionCtx})
	defer func() {
		cancelSession()
		b.sessionCtx.Store(&sessionCtxHolder{ctx: context.Background()})
		b.cmdGuard.Store(&cmdGuardHolder{g: disabledGuard})
	}()

	// --- Connect / disconnect lifecycle ---
	if err := dg.Open(); err != nil {
		return fmt.Errorf("failed to open Discord session: %w", err)
	}
	defer func() {
		b.log.Info().Msg("discord_session_close")
		dg.Close()
	}()

	// --- Internal system events wiring (command refresh etc.) ---
	go func() {
		for {
			select {
			case <-sessionCtx.Done():
				return
			case evt, ok := <-b.systemBus.Events():
				if !ok {
					return
				}
				if evt.Type == systemevents.EventRefreshCommands {
					go func() {
						// Prefer targeted refresh (one guild) when possible.
						if evt.GuildID != "" {
							if err := b.cmdManager.SyncGuildCommands(evt.GuildID); err != nil {
								b.log.Error().Str("guild_id", evt.GuildID).Err(err).Msg("commands_sync_failed")
							}
							return
						}
						b.cmdManager.SyncAllGuilds()
					}()
				}
			}
		}
	}()

	// --- Watchdog: WS silence (gateway receive loop appears dead) ---
	go watchdog.NewWSSilence(
		tracker,
		b.cfg.WSSilenceTimeout,
		dg.HeartbeatLatency,
		func(meta watchdog.WSSilenceMeta) {
			b.log.Warn().
				Dur("since_last_ws", meta.SinceLastWS).
				Dur("timeout", meta.Timeout).
				Dur("heartbeat_latency", meta.HeartbeatLatency).
				Msg("gateway_silent")
			notifyUnhealthy()
		},
		watchdog.WSSilenceOptions{SettleDelay: 15 * time.Second, Tick: 10 * time.Second},
	).Run(sessionCtx)

	// --- Watchdog: active API probe (hard check every 30s) ---
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
					b.log.Debug().Dur("heartbeat_latency", lat).Msg("heartbeat_latency_skipped")
					continue
				}
				if _, err := dg.User("@me"); err != nil {
					fails++
					b.log.Warn().Int("fails", fails).Err(err).Msg("api_probe_failed")
					if fails >= 3 {
						b.log.Warn().Int("fails", fails).Msg("api_probe_threshold")
						notifyUnhealthy()
						return
					}
				} else {
					if fails > 0 {
						b.log.Info().Int("fails", fails).Msg("api_probe_recovered")
					}
					fails = 0
					b.log.Debug().Dur("heartbeat_latency", lat).Msg("heartbeat_latency")
				}
			}
		}
	}()

	select {
	case <-ctx.Done():
		b.log.Info().Msg("shutdown_signal_received")
		b.stopAllPlayers()
		return nil
	case <-disconnected:
		return fmt.Errorf("%w: websocket disconnected", ErrSessionUnhealthy)
	}
}

// stopAllPlayers stops playback and disconnects voice for all guilds. Call on shutdown.
func (b *Bot) stopAllPlayers() {
	if b.voice != nil {
		b.voice.StopAllPlayers()
	}
	b.log.Info().Msg("players_all_stopped")
}

func (b *Bot) configureIntents() {
	b.dg.Identify.Intents = discordgo.IntentsAll
}

// IsSessionUnhealthyError reports whether an error means we should fast-restart the session.
func IsSessionUnhealthyError(err error) bool {
	return errors.Is(err, ErrSessionUnhealthy)
}
