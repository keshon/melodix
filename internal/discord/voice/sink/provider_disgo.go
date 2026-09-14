package sink

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/disgoorg/snowflake/v2"
	musicsink "github.com/keshon/melodix/pkg/music/sink"
	"github.com/rs/zerolog"
	davesession "github.com/thomas-vilte/dave-go/session"

	"github.com/disgoorg/disgo/voice"
)

// DisgoSinkProvider is DiscordSinkProvider's opposite number: the same
// contract, one guild, backed by disgo's voice stack and dave-go's E2EE
// instead of the vendored discordgo fork's. Which one a guild gets is chosen
// at startup, so a single build can be pointed at either and the difference
// attributed to the library rather than to the build.
type DisgoSinkProvider struct {
	bridge          *DisgoVoice
	getSession      SessionGetter
	guildID         string
	voiceReadyDelay time.Duration
	log             zerolog.Logger

	mu               sync.Mutex
	conn             voice.Conn
	dave             *davesession.Session
	currentChannelID string
}

// NewDisgoSinkProvider creates a sink provider for the given guild, sharing
// one bridge (and so one voice manager) with every other guild's provider.
func NewDisgoSinkProvider(bridge *DisgoVoice, getSession SessionGetter, guildID string, voiceReadyDelay time.Duration, log zerolog.Logger) *DisgoSinkProvider {
	if voiceReadyDelay <= 0 {
		voiceReadyDelay = 500 * time.Millisecond
	}
	return &DisgoSinkProvider{
		bridge:          bridge,
		getSession:      getSession,
		guildID:         guildID,
		voiceReadyDelay: voiceReadyDelay,
		log:             log.With().Str("component", "sink").Str("backend", "disgo").Logger(),
	}
}

// Sink joins the voice channel (or reuses the existing connection) and returns
// an AudioSink. target must be non-empty.
func (p *DisgoSinkProvider) Sink(target string) (musicsink.AudioSink, error) {
	if target == "" {
		return nil, fmt.Errorf("voice channel ID is required")
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.conn != nil && p.currentChannelID == target {
		return &DisgoSink{conn: p.conn, log: p.log}, nil
	}
	if p.conn != nil {
		p.releaseLocked()
	}

	dg := p.getSession()
	if dg == nil {
		return nil, fmt.Errorf("no Discord session")
	}
	guildID, err := snowflake.Parse(p.guildID)
	if err != nil {
		return nil, fmt.Errorf("parsing guild id: %w", err)
	}
	channelID, err := snowflake.Parse(target)
	if err != nil {
		return nil, fmt.Errorf("parsing channel id: %w", err)
	}

	conn, dave, err := p.bridge.Conn(dg, guildID)
	if err != nil {
		return nil, err
	}

	joinCtx, cancel := context.WithTimeout(context.Background(), voiceJoinTimeout)
	defer cancel()
	if err := conn.Open(joinCtx, channelID, false, true); err != nil {
		p.bridge.Remove(context.Background(), guildID)
		return nil, fmt.Errorf("failed to join voice channel: %w", err)
	}

	p.conn = conn
	p.dave = dave
	p.currentChannelID = target
	p.log.Info().Str("channel_id", target).Str("guild_id", p.guildID).Msg("voice_joined")

	if err := p.awaitEncryption(dave); err != nil {
		p.releaseLocked()
		return nil, err
	}

	return &DisgoSink{conn: conn, log: p.log}, nil
}

// awaitEncryption blocks until the connection may send, which on a channel
// using end-to-end encryption means until the MLS group has an epoch.
//
// The gate is ShouldHoldFrames rather than Ready, because Ready never becomes
// true on a channel that has no E2EE at all and waiting on it there would
// stall every join by the full timeout. ShouldHoldFrames is only true in the
// window where encryption is expected and not yet established — which is
// exactly the window where sending would put passthrough frames in front of
// receivers that will drop them.
//
// The sleep first is the same one the discordgo provider takes, and covers the
// same gap: the protocol version is not known until SELECT_PROTOCOL_ACK
// arrives, and before that ShouldHoldFrames cannot distinguish "no encryption
// here" from "not asked yet". It is a delay, not a synchronisation — if this
// ever reports a channel ready that was not, that race is where to look.
func (p *DisgoSinkProvider) awaitEncryption(dave *davesession.Session) error {
	time.Sleep(p.voiceReadyDelay)

	if dave == nil || !dave.ShouldHoldFrames() {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), daveReadyTimeout)
	defer cancel()

	waited, err := dave.WaitReady(ctx)
	if err != nil {
		state := dave.State()
		p.log.Error().
			Str("guild_id", p.guildID).
			Dur("waited", daveReadyTimeout).
			Int("protocol_version", int(state.ProtocolVersion)).
			Err(err).
			Msg("voice_encryption_unavailable")
		return fmt.Errorf("voice channel uses end-to-end encryption and it did not come up: %w", err)
	}

	p.log.Info().
		Str("guild_id", p.guildID).
		Dur("waited", waited).
		Msg("voice_encryption_ready")
	return nil
}

// ReleaseSink disconnects from the voice channel for the given target.
func (p *DisgoSinkProvider) ReleaseSink(target string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn == nil {
		return
	}
	if target != "" && p.currentChannelID != target {
		return
	}
	p.releaseLocked()
}

// InvalidateSink drops the connection without matching a target, so the next
// Sink rejoins.
func (p *DisgoSinkProvider) InvalidateSink() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn == nil {
		return
	}
	p.releaseLocked()
}

func (p *DisgoSinkProvider) releaseLocked() {
	guildID, err := snowflake.Parse(p.guildID)
	if err == nil {
		p.bridge.Remove(context.Background(), guildID)
	}
	p.conn = nil
	p.dave = nil
	p.currentChannelID = ""
}
