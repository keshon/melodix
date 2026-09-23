// Package playback holds what every CLI command that queues tracks does after,
// mirroring the bot's playback package: start the player if it is idle, and
// word a failed music.Add.
package playback

import (
	"errors"
	"fmt"

	"github.com/keshon/melodix/internal/cli"
	"github.com/keshon/melodix/internal/music"
	"github.com/keshon/melodix/pkg/music/player"
)

// Start starts playback when the player is idle, and otherwise says how many
// tracks were queued behind what is playing. A start is announced by the
// status printer, as the track begins.
func Start(c *cli.Context, added int) {
	p := c.Player()
	if p.IsPlaying() {
		c.Println(TracksAdded(added))
		return
	}
	err := p.PlayNext("")
	switch {
	case err == nil:
	case errors.Is(err, player.ErrNoTracksInQueue):
		c.Println("Nothing is in the queue to play.")
	default:
		c.Println("Playback error:", err)
	}
}

// TracksAdded is the line for tracks queued behind what is playing.
func TracksAdded(added int) string {
	switch {
	case added == 1:
		return "🎶 Added 1 track to the queue."
	case added > 1:
		return fmt.Sprintf("🎶 Added %d tracks to the queue.", added)
	default:
		return "🎶 Added to the queue."
	}
}

// AddError words a failed music.Add.
func AddError(c *cli.Context, err error) {
	switch {
	case errors.Is(err, music.ErrHistoryUnavailable):
		c.Println("Playback history is not available: the bot holds the data directory, or storage failed to open.")
	case errors.Is(err, music.ErrUnknownHistoryID):
		c.Println("Unknown history id. It may have been removed when the list was trimmed, or the id is wrong.")
	default:
		c.Println("Error:", err)
	}
}
