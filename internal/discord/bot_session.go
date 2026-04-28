package discord

import (
	"context"
	"errors"
	"fmt"
	"log"
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
)

// NewBot creates a Bot. Register any bot-dependent commands before calling Run.
func NewBot(cfg *config.Config, storage *storage.Storage) *Bot {
	b := &Bot{
		cfg:       cfg,
		storage:   storage,
		slashCmds: make(map[string][]*discordgo.ApplicationCommand),
		systemBus: systemevents.New(32),
	}
	// Voice service must outlive a single Discord session so playback/queues survive reconnects.
	b.voice = voice.New(func() *discordgo.Session {
		b.mu.RLock()
		s := b.dg
		b.mu.RUnlock()
		return s
	}, cfg, storage)
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
	b.cmdLogger = commandlogger.New(dg, b.storage)
	b.cmdManager = commandsync.NewSyncer(dg, commandkit.DefaultRegistry)

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
	var disconnectOnce sync.Once
	notifyDisconnect := func() {
		disconnectOnce.Do(func() {
			log.Println("[WARN] Discord session unhealthy — will restart session")
			// Soft-restart path: keep players/queues, but invalidate transport so they recover fast.
			if b.voice != nil {
				b.voice.InvalidateAllSinks()
			}
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
		log.Println("[INFO] Closing Discord session...")
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
								log.Printf("[ERR] Failed to refresh commands for guild %s: %v", evt.GuildID, err)
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
			log.Printf("[WARN] Gateway silent for %v (timeout=%v, heartbeat=%v) — reconnecting", meta.SinceLastWS, meta.Timeout, meta.HeartbeatLatency)
			notifyDisconnect()
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
		return fmt.Errorf("%w: websocket disconnected", ErrSessionUnhealthy)
	}
}

// stopAllPlayers stops playback and disconnects voice for all guilds. Call on shutdown.
func (b *Bot) stopAllPlayers() {
	if b.voice != nil {
		b.voice.StopAllPlayers()
	}
	log.Println("[INFO] All players stopped")
}

func (b *Bot) configureIntents() {
	b.dg.Identify.Intents = discordgo.IntentsAll
}

// IsSessionUnhealthyError reports whether an error means we should fast-restart the session.
func IsSessionUnhealthyError(err error) bool {
	return errors.Is(err, ErrSessionUnhealthy)
}
