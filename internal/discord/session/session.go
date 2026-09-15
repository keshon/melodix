// Package session opens and holds a gateway connection.
//
// Build a client, connect, log, and record what a watchdog needs to judge the
// connection. Nothing above it names a Discord library: commands reach Discord
// through cmdadapter's neutral types, and reply supplies the implementations.
package session

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/cache"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgo/voice"
	"github.com/rs/zerolog"
)

// Session is one disgo connection and the state a watchdog needs to judge it.
type Session struct {
	client *bot.Client
	log    zerolog.Logger

	mu sync.RWMutex
	// lastHeartbeatAck is when the gateway last acknowledged a heartbeat.
	//
	// Under discordgo this had to be read off the session behind the lock
	// that discordgo also holds across gateway reads, which is why reading it
	// needed a timeout and an abandoned goroutine: a black-holed socket parks
	// every reader. disgo delivers the ack as an event, so it is recorded
	// here on arrival and read without contending with anything.
	lastHeartbeatAck time.Time
}

// Options configure a session.
type Options struct {
	Token string
	Log   zerolog.Logger
	// Listeners are added before the gateway opens, so nothing is missed
	// between connecting and wiring.
	Listeners []bot.EventListener
	// VoiceManagerOpts configure the voice manager the client builds. This is
	// where the DAVE session registry gets in: the session cannot be reached
	// from a Conn in v0.19.6, so it has to be caught as each Conn is created.
	VoiceManagerOpts []voice.ManagerConfigOpt
}

// New builds a disgo client without connecting.
//
// Raw events are enabled for the caller's silence watchdog, which measures
// "anything at all arrived" rather than "an event we handle arrived" -- a
// gateway delivering only events this bot ignores is still alive, and treating
// that as silence is how a healthy session gets restarted.
func New(opts Options) (*Session, error) {
	s := &Session{log: opts.Log.With().Str("component", "disgo").Logger()}

	logger := slog.New(slog.NewTextHandler(logWriter{log: s.log}, &slog.HandlerOptions{
		Level: slogLevel(s.log.GetLevel()),
	}))

	listeners := []bot.EventListener{
		bot.NewListenerFunc(s.onHeartbeatAck),
	}
	listeners = append(listeners, opts.Listeners...)

	client, err := disgo.New(opts.Token,
		bot.WithLogger(logger),
		bot.WithGatewayConfigOpts(
			gateway.WithIntents(gateway.IntentsAll),
			gateway.WithEnableRawEvents(true),
			gateway.WithLogger(logger),
		),
		bot.WithCacheConfigOpts(
			cache.WithCaches(cache.FlagsAll),
		),
		bot.WithVoiceManagerConfigOpts(opts.VoiceManagerOpts...),
		bot.WithEventListeners(listeners...),
	)
	if err != nil {
		return nil, fmt.Errorf("building disgo client: %w", err)
	}

	s.client = client
	return s, nil
}

// Client is the underlying disgo client, for the backend packages that build
// their implementations on it.
func (s *Session) Client() *bot.Client { return s.client }

// Open connects the gateway.
func (s *Session) Open(ctx context.Context) error {
	if err := s.client.OpenGateway(ctx); err != nil {
		return fmt.Errorf("opening disgo gateway: %w", err)
	}
	return nil
}

// Close disconnects. It is bounded by ctx, because the reason a session is
// being torn down early is usually that something in it has stopped
// returning.
func (s *Session) Close(ctx context.Context) {
	s.client.Close(ctx)
}

// LastHeartbeatAck reports when the gateway last acknowledged a heartbeat, and
// whether it ever has.
//
// The bool is false only before the first ack, where a staleness check would
// otherwise read a zero time as "very stale" and restart a session that has
// merely just connected. Unlike the discordgo equivalent it cannot fail to
// answer, so there is no wedged-lock case for a caller to handle.
func (s *Session) LastHeartbeatAck() (time.Time, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastHeartbeatAck, !s.lastHeartbeatAck.IsZero()
}

// Latency is the round trip to the gateway.
func (s *Session) Latency() time.Duration {
	if s.client == nil || s.client.Gateway == nil {
		return 0
	}
	return s.client.Gateway.Latency()
}

func (s *Session) onHeartbeatAck(_ *events.HeartbeatAck) {
	now := time.Now()
	s.mu.Lock()
	s.lastHeartbeatAck = now
	s.mu.Unlock()
}

// logWriter turns disgo's slog output into one zerolog event per line, the
// same shape attachDiscordgoLogger gives the fork: the library's own text is a
// field rather than the event name, so every line stays greppable under one
// name.
type logWriter struct {
	log zerolog.Logger
}

func (w logWriter) Write(p []byte) (int, error) {
	line := strings.TrimRight(string(p), "\n")
	if strings.Contains(line, audioSendFailure) {
		// Not a fix, and should not be mistaken for one. disgo logs a UDP
		// write failure that is not a closed socket and carries on pulling at
		// 50Hz, so a whole track can be drained into a socket delivering
		// nothing while everything above reports normal playback. Melodix
		// cannot observe that error any other way and cannot act on it at all
		// -- so it is at least given a name worth counting and alerting on,
		// rather than being one line of library prose among thousands.
		w.log.Error().Str("raw", line).Msg("voice_audio_send_failed")
		return len(p), nil
	}
	w.log.Info().Str("raw", line).Msg("disgo_log")
	return len(p), nil
}

// audioSendFailure is disgo's own wording for a UDP write it could not make
// and did not act on (voice/audio_sender.go, handleErr). Matched as text
// because it reaches us as text: the sender logs it and returns, so there is
// no error value anywhere for melodix to catch.
const audioSendFailure = "failed to send audio"

// slogLevel maps the app's configured level onto disgo's, so LOG_LEVEL reaches
// the library rather than the library deciding for itself.
func slogLevel(l zerolog.Level) slog.Level {
	switch l {
	case zerolog.TraceLevel, zerolog.DebugLevel:
		return slog.LevelDebug
	case zerolog.InfoLevel:
		return slog.LevelInfo
	case zerolog.WarnLevel:
		return slog.LevelWarn
	default:
		return slog.LevelError
	}
}

// SlogLogger is the app logger in the shape disgo's own subsystems take, for
// callers that configure one (the voice manager) outside New.
func SlogLogger(log zerolog.Logger) *slog.Logger {
	return slog.New(slog.NewTextHandler(logWriter{log: log}, &slog.HandlerOptions{
		Level: slogLevel(log.GetLevel()),
	}))
}
