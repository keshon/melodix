package discord

import (
	"context"
	"fmt"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
	"github.com/keshon/command"
	"github.com/rs/zerolog"

	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/discord/disgolog"
	"github.com/keshon/melodix/internal/discord/disgoreply"
	"github.com/keshon/melodix/internal/discord/disgosession"
	"github.com/keshon/melodix/internal/discord/disgosync"
	"github.com/keshon/melodix/internal/discord/execguard"
	"github.com/keshon/melodix/internal/discord/voice/sink"
	musicsink "github.com/keshon/melodix/pkg/music/sink"
)

// disgoConn is disgo's connection.
type disgoConn struct {
	client     *bot.Client
	dave       *sink.DaveRegistry
	voiceDelay time.Duration
	log        zerolog.Logger
}

var _ conn = disgoConn{}

func (c disgoConn) API() cmdadapter.BotAPI {
	return disgoreply.NewSessionAPI(c.client)
}

// NewSinkProvider builds the audio path on disgo's voice manager.
//
// VOICE_BACKEND does not get a vote here. It chooses between two audio paths
// that both run on discordgo's gateway, which is what the spike proved is
// possible; running the fork's voice stack on a disgo gateway is the mirror
// image and nothing has ever needed it. A disgo gateway therefore always
// carries disgo's voice.
func (c disgoConn) NewSinkProvider(guildID string) musicsink.Provider {
	gid, err := snowflake.Parse(guildID)
	if err != nil {
		c.log.Error().Str("guild_id", guildID).Err(err).Msg("voice_guild_id_invalid")
		return deadSinkProvider{}
	}
	return sink.NewDisgoSinkProvider(c.client.VoiceManager, c.dave, gid, c.voiceDelay, c.log)
}

// runDisgoSession opens one session on disgo and blocks until ctx is
// cancelled or the session is judged unhealthy.
//
// It is the same shape as the discordgo one -- open, wire, watch, block --
// with two differences worth naming. Handlers are typed events rather than a
// function whose signature decides what it receives, so a handler that takes
// the wrong event is a compile error rather than one that never fires. And
// the heartbeat is an event, so the watchdog reads a timestamp instead of
// taking a lock the library holds across gateway reads.
func (b *Bot) runDisgoSession(ctx context.Context) error {
	disconnected := make(chan struct{})
	notifyUnhealthy := b.makeSessionUnhealthyNotifier(disconnected)

	// Built before the session opens so nothing is missed between connecting
	// and wiring. The syncer and logger need the client, which does not exist
	// yet, so they are filled in once it does.
	var (
		syncer *disgosync.Syncer
		logger *disgolog.Logger
	)

	dave := sink.NewDaveRegistry()

	session, err := disgosession.New(disgosession.Options{
		Token:            b.cfg.DiscordToken,
		Log:              b.log,
		VoiceManagerOpts: dave.ManagerOptions(disgosession.SlogLogger(b.log)),
		Listeners: []bot.EventListener{
			bot.NewListenerFunc(func(e *events.Ready) {
				b.onDisgoReady(e, syncer)
			}),
			bot.NewListenerFunc(func(e *events.GuildJoin) {
				b.onDisgoGuildJoin(e, syncer)
			}),
			bot.NewListenerFunc(func(e *events.ApplicationCommandInteractionCreate) {
				b.onDisgoApplicationCommand(e, syncer, logger)
			}),
			bot.NewListenerFunc(func(e *events.ComponentInteractionCreate) {
				b.onDisgoComponent(e, logger)
			}),
			bot.NewListenerFunc(b.onDisgoMessage),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}

	client := session.Client()
	syncer = disgosync.NewSyncer(client, command.DefaultRegistry, b.log)
	logger = disgolog.NewLogger(client, b.storage, b.log)

	b.setConn(disgoConn{
		client:     client,
		dave:       dave,
		voiceDelay: time.Duration(b.cfg.VoiceReadyDelayMs) * time.Millisecond,
		log:        b.log,
	})
	defer b.clearConn()

	b.cmdGuard.Store(&cmdGuardHolder{g: execguard.New(b.cfg.CommandTimeout, b.cfg.CommandParallelism)})

	sessionCtx, cancelSession := context.WithCancel(ctx)
	b.sessionCtx.Store(&sessionCtxHolder{ctx: sessionCtx})
	defer func() {
		cancelSession()
		b.sessionCtx.Store(&sessionCtxHolder{ctx: context.Background()})
		b.cmdGuard.Store(&cmdGuardHolder{g: disabledGuard})
	}()

	openCtx, cancelOpen := context.WithTimeout(ctx, 30*time.Second)
	defer cancelOpen()
	if err := session.Open(openCtx); err != nil {
		return fmt.Errorf("failed to open Discord session: %w", err)
	}
	defer func() {
		b.log.Info().Msg("discord_session_close")
		// Bounded and abandonable, for the same reason the discordgo close is:
		// disgo's Close tears down the voice manager, then the gateway, then
		// the REST rate limiter, each waiting on the last, and the usual
		// reason a session is being closed early is that one of them has
		// stopped answering.
		closeWithin("session_close", sessionCloseTimeout, b.log, func() {
			closeCtx, cancelClose := context.WithTimeout(context.Background(), sessionCloseTimeout)
			defer cancelClose()
			session.Close(closeCtx)
		})
	}()

	b.startDisgoHealthWatcher(sessionCtx, session, notifyUnhealthy)

	select {
	case <-ctx.Done():
		b.log.Info().Msg("shutdown_signal_received")
		closeWithin("stop_players", playersStopTimeout, b.log, b.stopAllPlayers)
		return nil
	case <-disconnected:
		return fmt.Errorf("%w: websocket disconnected", ErrSessionUnhealthy)
	}
}

// startDisgoHealthWatcher watches for a gateway that has stopped talking.
//
// It is one watcher rather than the fork's two. The second one existed to
// notice a session whose lock would never come free, which is a discordgo
// pathology: it holds the session write lock across gateway reads that carry
// no deadline, so a black-holed socket parks every reader behind it. disgo
// records the heartbeat as an event, so there is no lock to wedge and nothing
// to time out reading -- see disgosession.LastHeartbeatAck.
func (b *Bot) startDisgoHealthWatcher(
	ctx context.Context,
	session *disgosession.Session,
	notifyUnhealthy func(),
) {
	timeout := b.cfg.WSSilenceTimeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}

	go func() {
		// The settle delay matches the fork's: a session that has just
		// connected has no traffic yet, and restarting it for that would be a
		// loop rather than a recovery.
		select {
		case <-ctx.Done():
			return
		case <-time.After(15 * time.Second):
		}

		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				last, ok := session.LastEvent()
				if !ok {
					// Connected and nothing has arrived at all. Raw events are
					// on, so this really is silence rather than a filter.
					b.log.Warn().Msg("gateway_no_traffic_since_connect")
					notifyUnhealthy()
					return
				}
				since := time.Since(last)
				if since <= timeout {
					continue
				}

				ev := b.log.Warn().
					Dur("since_last_ws", since).
					Dur("timeout", timeout)
				if ack, acked := session.LastHeartbeatAck(); acked {
					ev = ev.Dur("since_last_heartbeat_ack", time.Since(ack))
				}
				ev.Msg("gateway_silent")
				notifyUnhealthy()
				return
			}
		}
	}()
}
