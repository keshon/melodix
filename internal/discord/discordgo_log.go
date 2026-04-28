package discord

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog"
)

// attachDiscordgoLogger routes discordgo internal logs through zerolog (global hook in discordgo).
//
// Every entry uses a fixed event name "discordgo_log" so it can be grepped reliably.
// The original printf-formatted message lands in the "raw" field, the discordgo
// severity in "dg_level", and the package marker in "component".
func attachDiscordgoLogger(log zerolog.Logger) {
	discordgo.Logger = func(msgL, caller int, format string, a ...interface{}) {
		_ = caller
		raw := fmt.Sprintf(format, a...)

		var (
			level string
			ev    *zerolog.Event
		)
		switch msgL {
		case discordgo.LogError:
			level, ev = "error", log.Error()
		case discordgo.LogWarning:
			level, ev = "warn", log.Warn()
		case discordgo.LogInformational:
			level, ev = "info", log.Info()
		case discordgo.LogDebug:
			level, ev = "debug", log.Debug()
		default:
			level, ev = "unknown", log.Info()
		}

		ev.Str("component", "discordgo").
			Str("dg_level", level).
			Str("raw", raw).
			Msg("discordgo_log")
	}
}
