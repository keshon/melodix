package discord

import (
	"fmt"
	"runtime"
	"strconv"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog"
)

// attachDiscordgoLogger routes discordgo internal logs through zerolog (global hook in discordgo).
func attachDiscordgoLogger(log zerolog.Logger) {
	discordgo.Logger = func(msgL, caller int, format string, a ...interface{}) {
		raw := fmt.Sprintf(format, a...)

		var (
			ev    *zerolog.Event
		)
		switch msgL {
		case discordgo.LogError:
			ev = log.Error()
		case discordgo.LogWarning:
			ev = log.Warn()
		case discordgo.LogInformational:
			ev = log.Info()
		case discordgo.LogDebug:
			ev = log.Debug()
		default:
			ev = log.Info()
		}

		// Ensure "at" points to the discordgo callsite (not this bridge).
		if pc, file, line, ok := runtime.Caller(caller); ok {
			_ = pc
			ev.Str("at", file+":"+strconv.Itoa(line))
		}

		ev.Str("raw", raw).Msg(raw)
	}
}
