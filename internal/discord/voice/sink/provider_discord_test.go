package sink

import (
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func TestDiscordSinkProviderInvalidateSink_Idempotent(t *testing.T) {
	p := NewDiscordSinkProvider(func() *discordgo.Session { return nil }, "guild1", 0)
	p.InvalidateSink()
	p.InvalidateSink()
	if p.vc != nil || p.currentChannelID != "" {
		t.Fatalf("expected cleared state after InvalidateSink, got vc=%v channel=%q", p.vc, p.currentChannelID)
	}
}

func TestNewDiscordSinkProvider_DefaultVoiceReadyDelay(t *testing.T) {
	p := NewDiscordSinkProvider(func() *discordgo.Session { return nil }, "g", 0)
	if p.voiceReadyDelay <= 0 {
		t.Fatal("expected positive default voiceReadyDelay")
	}
	if p.voiceReadyDelay != 500*time.Millisecond {
		t.Fatalf("unexpected default delay: %v", p.voiceReadyDelay)
	}
}
