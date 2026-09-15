// Package session opens and holds a gateway connection.
//
// Build a client, connect, log, and record what a watchdog needs to judge the
// connection. Nothing above it names a Discord library: commands reach Discord
// through adapter's neutral types, and reply supplies the implementations.
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

	logger := slog.New(slog.NewTextHandler(logWriter{log: s.log, frames: &frameCounter{}}, &slog.HandlerOptions{
		Level: slogLevel(s.log.GetLevel()),
	}))

	listeners := []bot.EventListener{
		bot.NewListenerFunc(s.onHeartbeatAck),
	}
	listeners = append(listeners, opts.Listeners...)

	client, err := disgo.New(opts.Token,
		bot.WithLogger(logger),
		bot.WithGatewayConfigOpts(
			gateway.WithIntents(botIntents),
			gateway.WithEnableRawEvents(true),
			gateway.WithLogger(logger),
		),
		bot.WithCacheConfigOpts(
			cache.WithCaches(botCaches),
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

// LastHeartbeatAck reports when the gateway last acknowledged a heartbeat, or
// the zero time before the first one arrives.
//
// Unlike the discordgo equivalent it cannot fail to answer: that one had to be
// read behind the lock discordgo also held across gateway reads with no
// deadline, so a black-holed socket parked every reader and the call itself
// could hang. disgo delivers the ack as an event.
func (s *Session) LastHeartbeatAck() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastHeartbeatAck
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

// botIntents is what the bot is actually told about.
//
// IntentGuilds carries guilds, channels and roles, which every permission
// check reads. IntentGuildVoiceStates is how the bot knows which channel a
// user is in, which is the whole of /play's first step.
//
// IntentGuildMembers is privileged and is here because permission checks read
// the member cache: a member missing from it is a command refused, not a
// command run with fewer rights. IntentGuildPresences and IntentMessageContent
// were both requested and neither was ever read -- presences by nothing at
// all, message content by a mention dispatch path no command ever handled.
// Asking for a privileged intent nobody reads is a gateway close code 4014
// waiting for whoever next sets this bot up without ticking all three boxes.
const botIntents = gateway.IntentGuilds |
	gateway.IntentGuildVoiceStates |
	gateway.IntentGuildMembers

// botCaches is what the bot actually reads back: guilds and their channels and
// roles for permission maths, members for the same, voice states to find a
// caller. FlagsAll additionally kept every message, presence, emoji, sticker,
// scheduled event, soundboard sound, thread member and stage instance the
// gateway ever mentioned, none of which is read anywhere.
const botCaches = cache.FlagGuilds |
	cache.FlagChannels |
	cache.FlagRoles |
	cache.FlagMembers |
	cache.FlagVoiceStates

// logWriter turns disgo's slog output into one zerolog event per line, the
// same shape attachDiscordgoLogger gives the fork: the library's own text is a
// field rather than the event name, so every line stays greppable under one
// name.
type logWriter struct {
	log zerolog.Logger
	// frames aggregates the one line dave-go emits per encrypted frame; nil
	// lets those lines through individually.
	frames *frameCounter
}

func (w logWriter) Write(p []byte) (int, error) {
	line := strings.TrimRight(string(p), "\n")
	if w.frames != nil && strings.Contains(line, frameEncrypted) {
		w.frames.count(w.log, line)
		return len(p), nil
	}
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

// frameEncrypted is dave-go's per-frame debug line. It is one line per 20ms of
// audio -- fifty a second, six and a half thousand in a two-minute track --
// and at LOG_LEVEL=debug it buries everything else in the file and everything
// else on the console.
//
// It is not noise, though. Whether a frame went out under the live epoch,
// under the previous one, or with no encryption at all is the only visible
// answer to "can the people in this channel actually hear this", and that
// question has already been wrong twice here. So it is counted, not dropped.
// Matched on the phrase rather than on the whole formatted field, because the
// surrounding format has already changed once in this project's own logs.
const frameEncrypted = "frame encrypted"

// frameSummaryEvery bounds how often the tally is reported: long enough that a
// track produces a handful of lines rather than thousands, short enough that a
// re-key window still shows up as one of them.
const frameSummaryEvery = 30 * time.Second

// frameCounter turns dave-go's per-frame line into a periodic tally.
//
// It counts only what that line actually says. A frame sent with no
// encryption at all is not in here, because dave-go's passthrough path logs
// nothing -- it increments State().Stats.PassthroughFrames and returns. That
// case is what the send-path hold in voicesink exists to prevent, and it is
// the session's own stat to answer for, not this bridge's to guess at.
type frameCounter struct {
	mu       sync.Mutex
	total    int
	retained int
	since    time.Time
}

func (c *frameCounter) count(log zerolog.Logger, line string) {
	c.mu.Lock()
	if c.since.IsZero() {
		c.since = time.Now()
	}
	c.total++
	if strings.Contains(line, "retained=true") {
		c.retained++
	}
	window := time.Since(c.since)
	if window < frameSummaryEvery {
		c.mu.Unlock()
		return
	}
	total, retained := c.total, c.retained
	c.total, c.retained, c.since = 0, 0, time.Time{}
	c.mu.Unlock()

	// under_previous_epoch is the one worth watching. dave-go keeps the
	// previous epoch's send key for ten seconds after a re-key, deliberately,
	// so listeners who have not processed the transition yet keep hearing
	// audio -- but it skips that when the new epoch added a member, because
	// somebody who was never in the old epoch holds none of its keys and
	// would hear nothing for the whole window. A count that stays high across
	// a join is that skip having failed.
	log.Info().
		Int("frames", total).
		Int("under_previous_epoch", retained).
		Dur("window", window).
		Msg("voice_frames_encrypted")
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

// SlogLogger is the app logger in the shape disgo's own subsystems take, for
// callers that configure one (the voice manager) outside New.
func SlogLogger(log zerolog.Logger) *slog.Logger {
	return slog.New(slog.NewTextHandler(logWriter{log: log}, &slog.HandlerOptions{
		Level: slogLevel(log.GetLevel()),
	}))
}
