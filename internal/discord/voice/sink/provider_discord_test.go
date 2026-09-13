package sink

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog"
)

func TestDiscordSinkProviderInvalidateSink_Idempotent(t *testing.T) {
	p := NewDiscordSinkProvider(func() *discordgo.Session { return nil }, "guild1", 0, zerolog.Nop())
	p.InvalidateSink()
	p.InvalidateSink()
	if p.vc != nil || p.currentChannelID != "" {
		t.Fatalf("expected cleared state after InvalidateSink, got vc=%v channel=%q", p.vc, p.currentChannelID)
	}
}

func TestNewDiscordSinkProvider_DefaultVoiceReadyDelay(t *testing.T) {
	p := NewDiscordSinkProvider(func() *discordgo.Session { return nil }, "g", 0, zerolog.Nop())
	if p.voiceReadyDelay <= 0 {
		t.Fatal("expected positive default voiceReadyDelay")
	}
	if p.voiceReadyDelay != 500*time.Millisecond {
		t.Fatalf("unexpected default delay: %v", p.voiceReadyDelay)
	}
}

// A channel that demands end-to-end encryption is the one join failure worth
// naming: it cannot be retried and has no fallback, so "failed to join voice
// channel" would send someone looking for a permission or a network problem
// they do not have.
func TestJoinFailureNamesTheEncryptionRequirement(t *testing.T) {
	err := joinFailure(fmt.Errorf("voice websocket closed: %w", discordgo.ErrVoiceE2EERequired))

	if !strings.Contains(err.Error(), "end-to-end encryption") {
		t.Fatalf("message should say what is wrong, got %q", err)
	}
	if !errors.Is(err, discordgo.ErrVoiceE2EERequired) {
		t.Fatal("wrapping must survive, so callers can still match on the cause")
	}
}

// Everything else keeps the generic wording rather than being guessed at.
func TestJoinFailureLeavesOtherErrorsAlone(t *testing.T) {
	cause := errors.New("context deadline exceeded")
	err := joinFailure(cause)

	if strings.Contains(err.Error(), "end-to-end encryption") {
		t.Fatalf("an unrelated failure should not be blamed on encryption: %q", err)
	}
	if !errors.Is(err, cause) {
		t.Fatal("the original cause should still be reachable")
	}
}
