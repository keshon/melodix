package discord

import (
	"context"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/pkg/music/sink"
	"github.com/keshon/melodix/pkg/music/stream"
)

// discordSink implements sink.AudioSink by encoding PCM to opus and sending to a voice connection.
type discordSink struct {
	vc *discordgo.VoiceConnection
}

func (d *discordSink) Stream(src io.ReadCloser, stop <-chan struct{}) error {
	return stream.StreamToDiscord(src, stop, d.vc)
}

// SessionGetter returns the current Discord session (used so providers stay valid across reconnects).
type SessionGetter func() *discordgo.Session

// DiscordSinkProvider implements sink.SinkProvider for a single guild. target is the voice channel ID.
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

// GetSink joins the voice channel (or reuses existing) and returns an AudioSink. target must be non-empty.
func (p *DiscordSinkProvider) GetSink(target string) (sink.AudioSink, error) {
	if target == "" {
		return nil, fmt.Errorf("voice channel ID is required")
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.vc != nil && p.currentChannelID == target {
		return &discordSink{vc: p.vc}, nil
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
	vc, err := dg.ChannelVoiceJoin(context.Background(), p.guildID, target, false, true)
	if err != nil {
		return nil, fmt.Errorf("failed to join voice channel: %w", err)
	}
	p.vc = vc
	p.currentChannelID = target
	log.Printf("[DiscordSink] Joined voice channel %s on guild %s", target, p.guildID)

	// Wait for voice encryption (op 4) before sending opus; sending before causes nil pointer in opusSender.
	time.Sleep(p.voiceReadyDelay)

	return &discordSink{vc: vc}, nil
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
