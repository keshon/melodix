package cli

import (
	"fmt"
	"io"

	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/player"
)

// StatusPrinter is the CLI's music.Hooks.Watch: the player's single status
// consumer. Like the bot's watcher it owns the transitions nobody asked for --
// a track starting, the queue running out -- while commands print what they
// did themselves.
func StatusPrinter(out io.Writer) func(scope string, p *player.Player) {
	return func(_ string, p *player.Player) {
		// played is whether a track has started since playback last finished.
		// Stop announces Stopped whether or not anything was playing -- quitting
		// an idle CLI stops the player too -- and a finish nobody saw start is
		// not one.
		played := false
		for status := range p.PlayerStatus {
			switch status {
			case player.StatusPlaying:
				played = true
				if track, ok := p.CurrentTrack(); ok {
					_, _ = fmt.Fprintln(out, "▶ Now playing:", track.Title)
				}
			case player.StatusStopped:
				// A transient Stopped fires between tracks; only the final
				// one means the queue ran out.
				if !played || p.IsPlaying() || len(p.Queue()) > 0 {
					continue
				}
				played = false
				_, _ = fmt.Fprintln(out, "⏹ Playback finished.")
			case player.StatusAdded, player.StatusError, player.StatusPaused, player.StatusResumed:
				// The command that queued tracks says so, a failure comes
				// through FailurePrinter, and the player supports neither
				// pause nor resume.
			}
		}
	}
}

// FailurePrinter is the CLI's music.Hooks.OnFailed.
func FailurePrinter(out io.Writer) func(scope string, track parsers.Track, err error) {
	return func(_ string, track parsers.Track, err error) {
		_, _ = fmt.Fprintf(out, "❌ Playback failed: %s: %v\n", track.Title, err)
	}
}
