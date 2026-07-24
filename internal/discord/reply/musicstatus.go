package reply

import (
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/sources"
)

// Music status embeds shared by the slash handlers (synchronous updates) and the
// voice service's per-player status watcher (async updates: auto-advance, queue end).
// They live here because voice.Service cannot import internal/command/music/common
// without an import cycle.

// NowPlayingEmbed builds the guild music status embed for a track that just started:
// a title/link line plus a line of inline-code "chips" (source · parser, duration or
// `live` for radio, artist when known). Embeds don't render -# subtext, so code spans
// are the chip look Discord gives us.
func NowPlayingEmbed(track *parsers.Track) *discordgo.MessageEmbed {
	var title, url string
	if track != nil {
		title, url = track.Title, track.URL
	}
	var desc string
	switch {
	case title != "" && url != "":
		desc = fmt.Sprintf("🎶 [%s](%s)", title, url)
	case title != "":
		desc = "🎶 " + title
	case url != "":
		desc = "🎶 " + url
	default:
		desc = "🎶 Unknown track"
	}
	if chips := trackChips(track); chips != "" {
		// Blank line: the only vertical spacing embed markdown offers.
		desc += "\n\n" + chips
	}
	return &discordgo.MessageEmbed{
		Title:       "▶️ Now Playing",
		Description: desc,
		Color:       EmbedColor,
	}
}

// trackChips renders the chip line; every chip is optional so an empty track
// degrades to no line at all.
func trackChips(track *parsers.Track) string {
	if track == nil {
		return ""
	}
	var chips []string

	source := track.SourceInfo.SourceName
	if source != "" {
		chips = append(chips, "`"+source+"`")
	}
	if track.Cached {
		// Served from the local cache — "cached" subsumes the parser/method chip.
		chips = append(chips, "`cached`")
	} else if track.CurrentParser != "" {
		chips = append(chips, "`"+track.CurrentParser+"`")
		if track.Passthrough {
			chips = append(chips, "`passthrough`")
		} else {
			chips = append(chips, "`ffmpeg`")
		}
	}

	switch {
	case source == sources.Radio:
		chips = append(chips, "`live`")
	case track.Duration > 0:
		chips = append(chips, "`"+formatDuration(track.Duration)+"`")
	}

	if track.Artist != "" {
		chips = append(chips, "`"+track.Artist+"`")
	}
	return strings.Join(chips, " ")
}

func formatDuration(d time.Duration) string {
	total := int(d.Round(time.Second).Seconds())
	h, m, s := total/3600, total/60%60, total%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

// TracksAddedEmbed builds the status embed for tracks queued while something is playing.
func TracksAddedEmbed() *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Title:       "🎶 Track(s) Added",
		Description: "Added to queue",
		Color:       EmbedColor,
	}
}

// PlaybackFinishedEmbed builds the status embed for natural queue end.
func PlaybackFinishedEmbed() *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Title:       "⏹ Playback Finished",
		Description: "Queue is empty.",
		Color:       EmbedColor,
	}
}
