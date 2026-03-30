package cli

import (
	"fmt"
	"io"
)

// PrintStartupHelp prints a compact command summary (shown once at startup).
func PrintStartupHelp(w io.Writer) {
	fmt.Fprintln(w, "Melodix CLI - commands:")
	fmt.Fprintln(w, "  play <url|query|id> [source] [parser]  enqueue or play from history ids")
	fmt.Fprintln(w, "  next, n, skip                          play next in queue")
	fmt.Fprintln(w, "  stop, s                                stop playback and clear queue")
	fmt.Fprintln(w, "  queue                                  show queue")
	fmt.Fprintln(w, "  status                                 playing / queue length")
	fmt.Fprintln(w, "  history [timeline|counts] [page]      playback history")
	fmt.Fprintln(w, "  help                                   usage for play and history")
	fmt.Fprintln(w, "  quit, exit, q                          exit")
	fmt.Fprintln(w, "Type `help` for more detail.")
}

// Background player status lines (ASCII).

func PrintNowPlaying(w io.Writer, title string) {
	fmt.Fprintln(w, "now playing:", title)
}

func PrintAddedToQueue(w io.Writer) {
	fmt.Fprintln(w, "added to queue")
}

func PrintStoppedStatus(w io.Writer) {
	fmt.Fprintln(w, "stopped")
}

func PrintPlaybackError(w io.Writer) {
	fmt.Fprintln(w, "playback error")
}
