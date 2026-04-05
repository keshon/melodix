package sink

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	musicsink "github.com/keshon/melodix/pkg/music/sink"
)

// SessionGetter returns the current Discord session (used so providers stay valid across reconnects).
type SessionGetter func() *discordgo.Session

// DiscordSinkProvider implements sink.Provider for a single guild. target is the voice channel ID.
type DiscordSinkProvider struct {
	getSession       SessionGetter
	guildID          string
	voiceReadyDelay  time.Duration
	mu               sync.Mutex
	vc               *discordgo.VoiceConnection
	currentChannelID string
}

// NewDiscordSinkProvider creates a sink provider for the given session getter and guild.
func NewDiscordSinkProvider(getSession SessionGetter, guildID string, voiceReadyDelay time.Duration) *DiscordSinkProvider {
	if voiceReadyDelay <= 0 {
		voiceReadyDelay = 500 * time.Millisecond
	}
	return &DiscordSinkProvider{
		getSession:      getSession,
		guildID:         guildID,
		voiceReadyDelay: voiceReadyDelay,
	}
}

// voiceJoinTimeout limits how long we wait for voice connection to become ready (e.g. no permission = no event).
const voiceJoinTimeout = 15 * time.Second

// Sink joins the voice channel (or reuses existing) and returns an AudioSink. target must be non-empty.
func (p *DiscordSinkProvider) Sink(target string) (musicsink.AudioSink, error) {
	if target == "" {
		return nil, fmt.Errorf("voice channel ID is required")
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.vc != nil && p.currentChannelID == target {
		return &DiscordSink{vc: p.vc}, nil
	}

	if p.vc != nil {
		if err := p.vc.Disconnect(context.Background()); err != nil {
			log.Printf("[DiscordSink] Disconnect error: %v", err)
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
		return nil, fmt.Errorf("failed to join voice channel: %w", err)
	}
	p.vc = vc
	p.currentChannelID = target
	log.Printf("[DiscordSink] Joined voice channel %s on guild %s", target, p.guildID)

	time.Sleep(p.voiceReadyDelay)

	return &DiscordSink{vc: vc}, nil
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
		log.Printf("[DiscordSink] Disconnect error: %v", err)
	}
	p.vc = nil
	p.currentChannelID = ""
}

// InvalidateSink clears the cached VoiceConnection without requiring a target match.
// The next Sink(target) will join again (e.g. after voice WebSocket loss while gateway reconnects).
func (p *DiscordSinkProvider) InvalidateSink() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.vc == nil {
		return
	}
	if err := p.vc.Disconnect(context.Background()); err != nil {
		log.Printf("[DiscordSink] Invalidate disconnect error: %v", err)
	}
	p.vc = nil
	p.currentChannelID = ""
}

