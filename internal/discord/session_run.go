package discord

import (
	"context"
	"fmt"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/events"
	disgovoice "github.com/disgoorg/disgo/voice"
	"github.com/keshon/command"

	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/discord/cmdlogger"
	"github.com/keshon/melodix/internal/discord/cmdsync"
	"github.com/keshon/melodix/internal/discord/execguard"
	"github.com/keshon/melodix/internal/discord/reply"
	"github.com/keshon/melodix/internal/discord/session"
	"github.com/keshon/melodix/internal/discord/voice/sink"
	"github.com/keshon/melodix/internal/discord/watchdog"
)

// clientConn is the live disgo connection.
type clientConn struct {
	client *bot.Client
	dave   *sink.DaveRegistry
}

var _ conn = clientConn{}

func (c clientConn) API() cmdadapter.BotAPI {
	return reply.NewSessionAPI(c.client)
}

// VoiceResources hands out this session's voice manager and DAVE registry.
func (c clientConn) VoiceResources() (disgovoice.Manager, *sink.DaveRegistry) {
	return c.client.VoiceManager, c.dave
}

// RunSession opens one Discord session and blocks until ctx is cancelled or
// the session is judged unhealthy (transient gateway reconnects do not exit
// this function).
func (b *Bot) RunSession(ctx context.Context) error {
	disconnected := make(chan struct{})
	notifyUnhealthy := b.makeSessionUnhealthyNotifier(disconnected)

	// Built before the session opens so nothing is missed between connecting
	// and wiring. The syncer and logger need the client, which does not exist
	// yet, so they are filled in once it does.
	var (
		syncer *cmdsync.Syncer
		logger *cmdlogger.Logger
	)

	dave := sink.NewDaveRegistry()
	tracker := watchdog.NewTracker()

	session, err := session.New(session.Options{
		Token:            b.cfg.DiscordToken,
		Log:              b.log,
		VoiceManagerOpts: dave.ManagerOptions(session.SlogLogger(b.log)),
		Listeners: []bot.EventListener{
			bot.NewListenerFunc(func(_ *events.Raw) { tracker.MarkWSNow() }),
			bot.NewListenerFunc(func(e *events.Ready) {
				tracker.MarkReadyNow()
				b.onReady(e, syncer)
			}),
			bot.NewListenerFunc(func(e *events.GuildJoin) {
				b.onGuildJoin(e, syncer)
			}),
			bot.NewListenerFunc(func(e *events.ApplicationCommandInteractionCreate) {
				b.onApplicationCommand(e, syncer, logger)
			}),
			bot.NewListenerFunc(func(e *events.ComponentInteractionCreate) {
				b.onComponentInteraction(e, logger)
			}),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}

	client := session.Client()
	syncer = cmdsync.NewSyncer(client, command.DefaultRegistry, b.log)
	logger = cmdlogger.NewLogger(client, b.storage, b.log)

	b.setConn(clientConn{client: client, dave: dave})
	defer b.clearConn()

	b.setGuard(execguard.New(b.cfg.CommandParallelism))

	sessionCtx, cancelSession := context.WithCancel(ctx)
	b.setSessionContext(sessionCtx)
	defer func() {
		cancelSession()
		b.setSessionContext(context.Background())
		b.setGuard(disabledGuard)
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
			closeCtx, cancelClose := context.WithTimeout(context.Background(), gatewayCloseBudget)
			defer cancelClose()
			session.Close(closeCtx)
		})
	}()

	b.startHealthWatcher(sessionCtx, session, tracker, notifyUnhealthy)

	select {
	case <-ctx.Done():
		b.log.Info().Msg("shutdown_signal_received")
		// Commands first: one of them may be the reason a player is playing,
		// and stopping playback underneath a command mid-answer is a worse
		// report than waiting a moment for it.
		closeWithin("drain_commands", commandsDrainTimeout, b.log, b.drainCommands)
		closeWithin("stop_players", playersStopTimeout, b.log, b.stopAllPlayers)
		return nil
	case <-disconnected:
		return fmt.Errorf("%w: websocket disconnected", ErrSessionUnhealthy)
	}
}

// startHealthWatcher watches for a gateway that has stopped talking.
//
// One watcher, not two. The fork needed a second to notice a session whose
// lock would never come free -- it held the session write lock across gateway
// reads that carried no deadline, so a black-holed socket parked every reader
// behind it. disgo records the heartbeat as an event, so there is no lock to
// wedge and nothing to time out reading.
func (b *Bot) startHealthWatcher(
	ctx context.Context,
	session *session.Session,
	tracker *watchdog.Tracker,
	notifyUnhealthy func(),
) {
	go watchdog.NewWSSilence(
		tracker,
		b.cfg.WSSilenceTimeout,
		session.Latency,
		func(meta watchdog.WSSilenceMeta) {
			b.log.Warn().
				Dur("since_last_ws", meta.SinceLastWS).
				Dur("since_last_heartbeat_ack", meta.SinceLastHeartbeatAck).
				Dur("heartbeat_latency", meta.HeartbeatLatency).
				Dur("timeout", meta.Timeout).
				Msg("gateway_silent")
			notifyUnhealthy()
		},
		watchdog.WSSilenceOptions{
			SettleDelay:      15 * time.Second,
			Tick:             10 * time.Second,
			LastHeartbeatAck: session.LastHeartbeatAck,
		},
	).Run(ctx)
}
