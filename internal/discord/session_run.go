package discord

import (
	"context"
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog"
	davesession "github.com/thomas-vilte/dave-go/session"

	"github.com/keshon/command"
	"github.com/keshon/melodix/internal/discord/cmdlogger"
	"github.com/keshon/melodix/internal/discord/cmdsync"
	"github.com/keshon/melodix/internal/discord/execguard"
	"github.com/keshon/melodix/internal/discord/voice/sink"
	"github.com/keshon/melodix/internal/discord/watchdog"
)

// RunSession opens one Discord session and blocks until ctx is cancelled or
// the session is judged unhealthy (transient gateway reconnects do not exit
// this function).
//
// Which library carries it is DISCORD_BACKEND, read once here. It cannot
// change while running, so rolling back is a restart -- which is the same act
// as redeploying the previous binary, and the reason this is scaffolding
// rather than a feature.
func (b *Bot) RunSession(ctx context.Context) error {
	backend, ok := ParseBackend(b.cfg.DiscordBackend)
	if !ok {
		b.log.Warn().Str("value", b.cfg.DiscordBackend).
			Str("using", string(backend)).Msg("discord_backend_unknown")
	}
	b.log.Info().Str("backend", string(backend)).Msg("discord_backend_selected")

	if backend == BackendDisgo {
		return b.runDisgoSession(ctx)
	}
	return b.runDiscordgoSession(ctx)
}

// runDiscordgoSession opens one session on the vendored fork.
func (b *Bot) runDiscordgoSession(ctx context.Context) error {
	dg, err := discordgo.New("Bot " + b.cfg.DiscordToken)
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	dg.LogLevel = discordgoLogLevel(b.cfg.LogLevel)
	// End-to-end encryption for voice. Set before Open: the session itself is
	// built later, when the gateway says a channel is encrypted, but the
	// factory has to be in place before any voice connection exists.
	//
	// dave-go is a full RFC 9420 implementation in pure Go, and "full" is the
	// operative word -- it can commit to an MLS group, so the bot holds its own
	// epoch while alone in a channel. The hand-rolled joiner this replaced
	// could not, which is what issue #11 was.
	dg.DAVESessionCreate = davesession.CreateFunc()

	b.mu.Lock()
	b.dg = dg
	b.cmdLogger = cmdlogger.NewLogger(dg, b.storage, b.log)
	b.cmdSyncer = cmdsync.NewSyncer(dg, command.DefaultRegistry, b.log)
	attachDiscordgoLogger(b.log)
	b.mu.Unlock()

	voiceBackend, ok := ParseBackend(b.cfg.VoiceBackend)
	if !ok {
		b.log.Warn().Str("value", b.cfg.VoiceBackend).
			Str("using", string(voiceBackend)).Msg("voice_backend_unknown")
	}
	b.log.Info().Str("voice_backend", string(voiceBackend)).Msg("voice_backend_selected")

	dgConn := discordgoConn{
		dg:         dg,
		voiceDelay: time.Duration(b.cfg.VoiceReadyDelayMs) * time.Millisecond,
		log:        b.log,
	}
	if voiceBackend == BackendDisgo {
		dgConn.bridge = sink.NewDisgoVoice(b.log)
	}
	b.setConn(dgConn)
	defer b.clearConn()

	b.cmdGuard.Store(&cmdGuardHolder{g: execguard.New(b.cfg.CommandTimeout, b.cfg.CommandParallelism)})

	tracker := watchdog.NewTracker()
	disconnected := make(chan struct{})
	notifyUnhealthy := b.makeSessionUnhealthyNotifier(disconnected)

	b.wireSessionHandlers(dg, tracker)

	sessionCtx, cancelSession := context.WithCancel(ctx)
	b.sessionCtx.Store(&sessionCtxHolder{ctx: sessionCtx})
	defer func() {
		cancelSession()
		b.sessionCtx.Store(&sessionCtxHolder{ctx: context.Background()})
		b.cmdGuard.Store(&cmdGuardHolder{g: disabledGuard})
	}()

	if err := dg.Open(); err != nil {
		return fmt.Errorf("failed to open Discord session: %w", err)
	}
	defer func() {
		b.log.Info().Msg("discord_session_close")
		closeSession(dg, sessionCloseTimeout, b.log)
	}()

	b.startSessionHealthWatchers(sessionCtx, dg, tracker, notifyUnhealthy)

	select {
	case <-ctx.Done():
		b.log.Info().Msg("shutdown_signal_received")
		b.stopAllPlayers()
		return nil
	case <-disconnected:
		return fmt.Errorf("%w: websocket disconnected", ErrSessionUnhealthy)
	}
}

// sessionCloseTimeout bounds the teardown of one session. Closing takes the
// session mutex, and the usual reason a session is being torn down early is
// that a watchdog found nothing will ever release it — see lastHeartbeatAck.
const sessionCloseTimeout = 15 * time.Second

// closeSession closes dg, abandoning it if the close does not return.
//
// Do NOT go back to a bare dg.Close() here. It is the last thing RunSession
// does, so a close that blocks blocks the restart loop in main with it, and
// the bot that a watchdog just correctly declared dead never comes back. What
// leaks instead is one parked goroutine and one socket the kernel reaps: the
// next RunSession builds a fresh *discordgo.Session and owes this one nothing.
func closeSession(dg *discordgo.Session, timeout time.Duration, log zerolog.Logger) {
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		_ = dg.Close()
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-closed:
	case <-timer.C:
		log.Warn().Dur("timeout", timeout).Msg("discord_session_close_abandoned")
	}
}
