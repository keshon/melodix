package discordreply

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
)

// Music status embeds shared by the slash handlers (synchronous updates) and the
// voice service's per-player status watcher (async updates: auto-advance, queue end).
// They live here because voice.Service cannot import internal/command/music/common
// without an import cycle.

// NowPlayingEmbed builds the guild music status embed for a track that just started.
func NowPlayingEmbed(title, url string) *discordgo.MessageEmbed {
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
	return &discordgo.MessageEmbed{
		Title:       "▶️ Now Playing",
		Description: desc,
		Color:       EmbedColor,
	}
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
