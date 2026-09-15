package voicesink

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/snowflake/v2"
	"github.com/rs/zerolog"

	"github.com/keshon/melodix/pkg/music/sink"
)

// ErrNoSession means there is no gateway session to join a voice channel with,
// which is a normal state between reconnects rather than a failure.
var ErrNoSession = errors.New("sink: no Discord session")

// Resources reaches the parts of the live gateway session a voice connection
// is built from. ok is false between sessions.
//
// It is a function rather than two fields because a Provider outlives any
// session and both of those die with one: the voice manager belongs to a
// bot.Client and closes over that client's gateway, and the DAVE registry is
// built fresh per session. Holding either meant a provider kept using a
// manager whose gateway was shut, whose joins fail immediately and forever --
// which no invalidation fixed, because what was invalidated was the
// connection and what was stale was the thing that makes connections.
type Resources func() (manager voice.Manager, dave *DaveRegistry, ok bool)

// Provider is one guild's audio path: join a voice channel, hand back a sink
// that forwards the track's Opus packets, and leave again. E2EE comes from
// dave-go, which is pure Go -- see DaveRegistry for why that matters.
//
// It lives for the process, like the player that holds it, and resolves the
// session it needs on every acquisition.
type Provider struct {
	resolve         Resources
	guildID         snowflake.ID
	voiceReadyDelay time.Duration
	log             zerolog.Logger

	mu sync.Mutex
	// openedConn is the connection this provider opened, kept to recognise it
	// again rather than to read state off. disgo owns a connection's lifetime
	// and removes it from the manager without telling anyone -- a voice
	// websocket closing on a code it cannot resume from is enough -- so
	// "connected" is a question only the manager can answer, and this is what
	// makes its answer comparable.
	openedConn voice.Conn
	// currentChannelID is what we asked for, which is ours to remember. The
	// connection's own view of it is written from the gateway goroutine
	// without a lock, so it is not something to read back.
	currentChannelID string
}

// NewProvider creates a sink provider for one guild. resolve reaches the live
// session; nothing about a session is held here.
func NewProvider(
	resolve Resources,
	guildID snowflake.ID,
	voiceReadyDelay time.Duration,
	log zerolog.Logger,
) *Provider {
	if voiceReadyDelay <= 0 {
		voiceReadyDelay = 500 * time.Millisecond
	}
	return &Provider{
		resolve:         resolve,
		guildID:         guildID,
		voiceReadyDelay: voiceReadyDelay,
		log:             log.With().Str("component", "sink").Str("backend", "disgo").Logger(),
	}
}

var _ sink.Provider = (*Provider)(nil)

// Sink joins the voice channel (or reuses the existing connection) and returns
// an AudioSink. target must be non-empty.
func (p *Provider) Sink(target string) (sink.AudioSink, error) {
	if target == "" {
		return nil, fmt.Errorf("voice channel ID is required")
	}
	channelID, err := snowflake.Parse(target)
	if err != nil {
		return nil, fmt.Errorf("parsing channel id: %w", err)
	}

	manager, dave, ok := p.resolve()
	if !ok {
		return nil, ErrNoSession
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// The manager is asked, not remembered. A connection this provider opened
	// may already have been closed and deregistered by disgo, and reusing it
	// hands back a sink over a socket that will never carry a packet.
	if p.openedConn != nil {
		switch {
		case manager.GetConn(p.guildID) != p.openedConn:
			p.log.Info().Str("guild_id", p.guildID.String()).Msg("voice_conn_gone_rejoining")
			p.forgetLocked(dave)
		case p.currentChannelID != target:
			p.releaseLocked(manager, dave)
		default:
			return p.sinkLocked(manager, dave), nil
		}
	}

	conn := manager.CreateConn(p.guildID)

	joinCtx, cancel := context.WithTimeout(context.Background(), voiceJoinTimeout)
	defer cancel()
	if err := conn.Open(joinCtx, channelID, false, true); err != nil {
		p.removeConn(manager, dave)
		return nil, fmt.Errorf("failed to join voice channel: %w", err)
	}

	p.openedConn = conn
	p.currentChannelID = target
	p.log.Info().Str("channel_id", target).Str("guild_id", p.guildID.String()).Msg("voice_joined")

	if err := p.awaitEncryption(dave); err != nil {
		p.releaseLocked(manager, dave)
		return nil, err
	}

	return p.sinkLocked(manager, dave), nil
}

// sinkLocked builds the sink for the connection this provider currently holds.
// The DAVE session is looked up per acquisition for the same reason the
// manager is: it belongs to the connection, and a rejoin builds a new one.
func (p *Provider) sinkLocked(manager voice.Manager, dave *DaveRegistry) sink.AudioSink {
	return &Sink{
		conn:    p.openedConn,
		manager: manager,
		guildID: p.guildID,
		dave:    gate(dave, p.guildID),
		log:     p.log,
	}
}

// gate is the guild's DAVE session as the send path's hold gate, or nil when
// no session was built. The nil is returned explicitly rather than by
// assigning the pointer, so a missing session is a nil interface rather than
// a non-nil one wrapping a nil receiver.
func gate(dave *DaveRegistry, guildID snowflake.ID) daveGate {
	if dave == nil {
		return nil
	}
	s := dave.Session(guildID)
	if s == nil {
		return nil
	}
	return s
}

// awaitEncryption blocks until the connection may send, which on a channel
// using end-to-end encryption means until the MLS group has an epoch.
//
// This is a join-time check, not the safety mechanism: readiness is not a
// property of joining but of the current epoch, and it is lost again on every
// re-key. What keeps unprotected frames off the wire is the per-frame gate in
// frameProvider. What this still buys is failing a join into a channel whose
// encryption never comes up at all, rather than starting a track that would
// spend its whole budget held.
//
// The gate is ShouldHoldFrames rather than Ready, because Ready never becomes
// true on a channel that has no E2EE at all and waiting on it there would
// stall every join by the full timeout. ShouldHoldFrames is only true in the
// window where encryption is expected and not yet established -- which is
// exactly the window where sending would put unprotected frames in front of
// receivers that will drop them.
//
// The sleep first is the same one the discordgo provider takes, and covers the
// same gap: the protocol version is not known until SELECT_PROTOCOL_ACK
// arrives, and before that ShouldHoldFrames cannot distinguish "no encryption
// here" from "not asked yet". It is a delay, not a synchronisation -- if this
// ever reports a channel ready that was not, that race is where to look.
func (p *Provider) awaitEncryption(registry *DaveRegistry) error {
	time.Sleep(p.voiceReadyDelay)

	if registry == nil {
		return nil
	}
	dave := registry.Session(p.guildID)
	if dave == nil || !dave.ShouldHoldFrames() {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), daveReadyTimeout)
	defer cancel()

	waited, err := dave.WaitReady(ctx)
	if err != nil {
		state := dave.State()
		p.log.Error().
			Str("guild_id", p.guildID.String()).
			Dur("waited", daveReadyTimeout).
			Int("protocol_version", int(state.ProtocolVersion)).
			Err(err).
			Msg("voice_encryption_unavailable")
		return fmt.Errorf("voice channel uses end-to-end encryption and it did not come up: %w", err)
	}

	p.log.Info().
		Str("guild_id", p.guildID.String()).
		Dur("waited", waited).
		Msg("voice_encryption_ready")
	return nil
}

// ReleaseSink disconnects from the voice channel for the given target.
func (p *Provider) ReleaseSink(target string) {
	p.release(target)
}

// InvalidateSink drops the connection without matching a target, so the next
// Sink rejoins.
func (p *Provider) InvalidateSink() {
	p.release("")
}

func (p *Provider) release(target string) {
	manager, dave, ok := p.resolve()

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.openedConn == nil {
		return
	}
	if target != "" && p.currentChannelID != target {
		return
	}
	if !ok {
		// The session that owned this connection is gone, and with it the
		// connection. There is nothing left to close politely.
		p.forgetLocked(nil)
		return
	}
	if manager.GetConn(p.guildID) != p.openedConn {
		// disgo has already taken it, which it does without telling anyone.
		// Closing it again would spend the close budget waiting on a gateway
		// that is not there, and this path runs on a command worker -- so it
		// would be the caller's wait, not a background one. The DAVE session
		// still has to go: nothing else will close it.
		p.log.Info().Str("guild_id", p.guildID.String()).Msg("voice_conn_already_gone")
		p.forgetLocked(dave)
		return
	}
	p.releaseLocked(manager, dave)
}

func (p *Provider) releaseLocked(manager voice.Manager, dave *DaveRegistry) {
	ctx, cancel := context.WithTimeout(context.Background(), voiceCloseTimeout)
	defer cancel()
	p.openedConn.Close(ctx)
	p.removeConn(manager, dave)
	p.forgetLocked(nil)
}

// forgetLocked drops this provider's record of a connection. Passing a
// registry also closes the guild's DAVE session, which is what the path disgo
// tears down behind our back needs: dave-go arms recovery watchdogs that keep
// re-arming invalidations on a channel the bot has left until they expire.
func (p *Provider) forgetLocked(dave *DaveRegistry) {
	if dave != nil {
		if err := dave.Forget(p.guildID); err != nil {
			p.log.Warn().Str("guild_id", p.guildID.String()).Err(err).Msg("dave_session_close_failed")
		}
	}
	p.openedConn = nil
	p.currentChannelID = ""
}

// removeConn drops the manager's record and the guild's DAVE session. A fresh
// Conn per channel is deliberate: the fork's reuse of one connection across
// channels kept the previous channel's state, which was one of the five bugs
// that made this migration worth doing.
func (p *Provider) removeConn(manager voice.Manager, dave *DaveRegistry) {
	manager.RemoveConn(p.guildID)
	if dave == nil {
		return
	}
	if err := dave.Forget(p.guildID); err != nil {
		p.log.Warn().Str("guild_id", p.guildID.String()).Err(err).Msg("dave_session_close_failed")
	}
}

// voiceCloseTimeout bounds a disconnect. Closing waits on the voice gateway,
// and the usual reason one is being closed is that it has stopped answering.
const voiceCloseTimeout = 10 * time.Second
