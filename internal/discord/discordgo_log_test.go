package discord

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

// The case that matters is debug: the library filters on its own scale before
// the bridge sees a line, so a level that does not reach LogDebug silently
// discards the voice and DAVE traces a bug report is asked for.
func TestDiscordgoLogLevelCarriesDebugThrough(t *testing.T) {
	for _, level := range []string{"debug", "DEBUG", "trace"} {
		if got := discordgoLogLevel(level); got != discordgo.LogDebug {
			t.Errorf("%q mapped to %d, want LogDebug (%d)", level, got, discordgo.LogDebug)
		}
	}
}

func TestDiscordgoLogLevelMapsTheRest(t *testing.T) {
	cases := map[string]int{
		"info":     discordgo.LogInformational,
		"warn":     discordgo.LogWarning,
		"error":    discordgo.LogError,
		"fatal":    discordgo.LogError,
		"panic":    discordgo.LogError,
		"":         discordgo.LogInformational,
		"nonsense": discordgo.LogInformational,
	}
	for level, want := range cases {
		if got := discordgoLogLevel(level); got != want {
			t.Errorf("%q mapped to %d, want %d", level, got, want)
		}
	}
}
