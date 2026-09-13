package sink

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	musicsink "github.com/keshon/melodix/pkg/music/sink"
	"github.com/rs/zerolog"
)

// SessionGetter returns the current Discord session (used so providers stay
// valid across reconnects).
type SessionGetter func() *discordgo.Session

// DiscordSinkProvider implements sink.Provider for a single guild. target is
// the voice channel ID.
type DiscordSinkProvider struct {
	getSession       SessionGetter
	guildID          string
	voiceReadyDelay  time.Duration
	log              zerolog.Logger
	mu               sync.Mutex
	vc               *discordgo.VoiceConnection
	currentChannelID string
}

// NewDiscordSinkProvider creates a sink provider for the given session getter
// and guild.
func NewDiscordSinkProvider(getSession SessionGetter, guildID string, voiceReadyDelay time.Duration, log zerolog.Logger) *DiscordSinkProvider {
	if voiceReadyDelay <= 0 {
		voiceReadyDelay = 500 * time.Millisecond
	}
	return &DiscordSinkProvider{
		getSession:      getSession,
		guildID:         guildID,
		voiceReadyDelay: voiceReadyDelay,
		log:             log.With().Str("component", "sink").Logger(),
	}
}

// voiceJoinTimeout limits how long we wait for voice connection to become ready
// (e.g. no permission = no event).
const voiceJoinTimeout = 15 * time.Second

// joinFailure explains a refused voice join, where the reason is worth more
// than the fact.
//
// Discord has required end-to-end encryption on every non-stage voice channel
// since March 2026, and a client that does not negotiate it is closed with code
// 4017. There is no version to fall back to and nothing to retry, so the one
// thing worth saying is which of those two situations this is.
func joinFailure(err error) error {
	if errors.Is(err, discordgo.ErrVoiceE2EERequired) {
		return fmt.Errorf("this voice channel requires end-to-end encryption and the bot could not negotiate it: %w", err)
	}
	return fmt.Errorf("failed to join voice channel: %w", err)
}

// daveReadyTimeout bounds the wait for end-to-end encryption to come up on a
// channel that uses it. The MLS exchange is a handful of round trips and
// settles in well under a second; ten is long enough that a slow link is not
// mistaken for a broken handshake.
const daveReadyTimeout = 10 * time.Second

// Sink joins the voice channel (or reuses existing) and returns an AudioSink.
// target must be non-empty.
func (p *DiscordSinkProvider) Sink(target string) (musicsink.AudioSink, error) {
	if target == "" {
		return nil, fmt.Errorf("voice channel ID is required")
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.vc != nil && p.currentChannelID == target {
		return &DiscordSink{vc: p.vc, log: p.log}, nil
	}

	if p.vc != nil {
		if err := p.vc.Disconnect(context.Background()); err != nil {
			p.log.Warn().Str("phase", "rejoin").Err(err).Msg("voice_disconnect_failed")
		}
		p.vc = nil
		p.currentChannelID = ""
	}

	dg := p.getSession()
	if dg == nil {
		return nil, fmt.Errorf("no Discord session")
	}
	joinCtx, cancel := context.WithTimeout(context.Background(), voiceJoinTimeout)
	defer cancel()
	vc, err := dg.ChannelVoiceJoin(joinCtx, p.guildID, target, false, true)
	if err != nil {
		return nil, joinFailure(err)
	}
	p.vc = vc
	p.currentChannelID = target
	p.log.Info().Str("channel_id", target).Str("guild_id", p.guildID).Msg("voice_joined")

	time.Sleep(p.voiceReadyDelay)

	if err := p.awaitEncryption(vc); err != nil {
		if derr := vc.Disconnect(context.Background()); derr != nil {
			p.log.Warn().Str("phase", "dave").Err(derr).Msg("voice_disconnect_failed")
		}
		p.vc = nil
		p.currentChannelID = ""
		return nil, err
	}

	return &DiscordSink{vc: vc, log: p.log}, nil
}

// awaitEncryption blocks until the voice connection can encrypt, on a channel
// where Discord requires it.
//
// Discord puts a channel into end-to-end encryption (DAVE) when every
// participant claims to support it, and from that point it expects encrypted
// frames. The connection cannot produce them until the MLS group is
// established, and sending anyway does not merely fail for us: Discord closes
// the connection within seconds, the rejoin re-keys the group, and while that
// repeats nobody in the channel can hear anybody. Refusing to play is the mild
// failure here — one guild gets an error message instead of a whole voice
// channel going silent.
//
// WaitForDAVEReady returns immediately on a channel that is not encrypted, so
// this costs nothing in the ordinary case.
func (p *DiscordSinkProvider) awaitEncryption(vc *discordgo.VoiceConnection) error {
	ctx, cancel := context.WithTimeout(context.Background(), daveReadyTimeout)
	defer cancel()

	if err := vc.WaitForDAVEReady(ctx); err != nil {
		p.log.Error().
			Str("guild_id", p.guildID).
			Dur("waited", daveReadyTimeout).
			Err(err).
			Msg("voice_encryption_unavailable")
		return fmt.Errorf("voice channel uses end-to-end encryption and it did not come up: %w", err)
	}
	return nil
}

// ReleaseSink disconnects from the voice channel for the given target.
func (p *DiscordSinkProvider) ReleaseSink(target string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.vc == nil {
		return
	}
	if target != "" && p.currentChannelID != target {
		return
	}
	if err := p.vc.Disconnect(context.Background()); err != nil {
		p.log.Warn().Str("phase", "release").Err(err).Msg("voice_disconnect_failed")
	}
	p.vc = nil
	p.currentChannelID = ""
}

// InvalidateSink clears the cached VoiceConnection without requiring a target
// match. The next Sink(target) will join again (e.g. after voice WebSocket loss
// while gateway reconnects).
func (p *DiscordSinkProvider) InvalidateSink() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.vc == nil {
		return
	}
	if err := p.vc.Disconnect(context.Background()); err != nil {
		p.log.Warn().Str("phase", "invalidate").Err(err).Msg("voice_disconnect_failed")
	}
	p.vc = nil
	p.currentChannelID = ""
}
