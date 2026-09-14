// Package disgosession opens and holds a disgo gateway connection.
//
// It is the disgo half of what internal/discord does with discordgo: build a
// client, connect, log, and report when the connection stops being worth
// keeping. Nothing above it names disgo -- commands reach Discord through
// cmdadapter's neutral types, and this package supplies the implementations.
package disgosession

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
	// lastEvent is when any gateway traffic last arrived, which is what the
	// silence watchdog actually measures.
	lastEvent time.Time
}

// Options configure a session.
type Options struct {
	Token string
	Log   zerolog.Logger
	// Listeners are added before the gateway opens, so nothing is missed
	// between connecting and wiring.
	Listeners []bot.EventListener
}

// New builds a disgo client without connecting.
//
// Raw events are enabled because the silence watchdog measures "anything at
// all arrived", not "an event we handle arrived" -- a gateway delivering only
// events this bot ignores is still alive, and treating that as silence is how
// a healthy session gets restarted.
func New(opts Options) (*Session, error) {
	s := &Session{log: opts.Log.With().Str("component", "disgo").Logger()}

	logger := slog.New(slog.NewTextHandler(logWriter{log: s.log}, &slog.HandlerOptions{
		Level: slogLevel(s.log.GetLevel()),
	}))

	listeners := []bot.EventListener{
		bot.NewListenerFunc(s.onRaw),
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

// LastEvent reports when gateway traffic last arrived.
func (s *Session) LastEvent() (time.Time, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastEvent, !s.lastEvent.IsZero()
}

// Latency is the round trip to the gateway.
func (s *Session) Latency() time.Duration {
	if s.client == nil || s.client.Gateway == nil {
		return 0
	}
	return s.client.Gateway.Latency()
}

func (s *Session) onRaw(_ *events.Raw) {
	s.mu.Lock()
	s.lastEvent = time.Now()
	s.mu.Unlock()
}

func (s *Session) onHeartbeatAck(_ *events.HeartbeatAck) {
	now := time.Now()
	s.mu.Lock()
	s.lastHeartbeatAck = now
	s.lastEvent = now
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
	w.log.Info().Str("raw", strings.TrimRight(string(p), "\n")).Msg("disgo_log")
	return len(p), nil
}

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
