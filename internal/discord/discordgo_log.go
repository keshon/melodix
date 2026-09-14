package discord

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog"
)

// attachDiscordgoLogger routes discordgo internal logs through zerolog (global
// hook in discordgo).
func attachDiscordgoLogger(log zerolog.Logger) {
	discordgo.Logger = func(msgL, caller int, format string, a ...interface{}) {
		raw := fmt.Sprintf(format, a...)

		var (
			ev *zerolog.Event
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
		if _, file, line, ok := runtime.Caller(caller); ok {
			ev.Str("at", file+":"+strconv.Itoa(line))
		}

		// The upstream text is a field, not the event name: it is arbitrary
		// prose from the library and would make every line its own unsearchable
		// event. One name, one field, greppable like every other event here.
		ev.Str("raw", raw).Msg("discordgo_log")
	}
}

// discordgoLogLevel maps the app's log level onto discordgo's, which is a
// separate scale the library filters on before a line ever reaches the bridge
// above. Pinning it at LogInformational is why issue #11 could not be read:
// the reporter ran LOG_LEVEL=debug as asked, and the per-opcode DAVE trace
// that would have named the fault (LogDebug, in the fork's
// handleDAVEBinary) was dropped inside discordgo before zerolog saw it.
// Don't hardcode a level here again — asking a user for debug logs has to
// produce them.
func discordgoLogLevel(appLevel string) int {
	switch strings.ToLower(appLevel) {
	case "trace", "debug":
		return discordgo.LogDebug
	case "warn":
		return discordgo.LogWarning
	case "error", "fatal", "panic":
		return discordgo.LogError
	default:
		return discordgo.LogInformational
	}
}
