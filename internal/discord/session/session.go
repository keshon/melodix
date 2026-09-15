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

	logger := slog.New(&bridge{log: s.log, frames: &frameCounter{}})

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
