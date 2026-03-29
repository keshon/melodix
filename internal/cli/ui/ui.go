// Package ui formats CLI output: help, history tables, and status lines (ASCII only).
package ui

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/keshon/melodix/internal/command/music"
)

const sectionUnderlineMax = 80

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

// PrintHelpDetail prints expanded usage for play and history.
func PrintHelpDetail(w io.Writer) {
	fmt.Fprintln(w, "play:")
	fmt.Fprintln(w, "  play <url|query|path> [source] [parser]")
	fmt.Fprintln(w, "    Enqueue a URL, search query, or local path. Optional source and parser override defaults.")
	fmt.Fprintln(w, "  play <id> <id> ...")
	fmt.Fprintln(w, "    Enqueue tracks from history by numeric id (space-separated ids).")
	fmt.Fprintln(w, "history:")
	fmt.Fprintln(w, "  history [timeline|counts] [page]")
	fmt.Fprintln(w, "    timeline - chronological plays (default). counts - grouped by URL with play counts.")
	fmt.Fprintln(w, "    page is 1-based.")
}

// PrintSectionHeader prints a title and an underline (width capped).
func PrintSectionHeader(w io.Writer, title string) {
	fmt.Fprintln(w, title)
	under := len(title)
	if under > sectionUnderlineMax {
		under = sectionUnderlineMax
	}
	fmt.Fprintln(w, strings.Repeat("-", under))
}

// PrintHistoryPage renders a history page with tabwriter columns and footer line.
func PrintHistoryPage(w io.Writer, view string, res *music.HistoryPageResult) {
	if res == nil {
		return
	}
	PrintSectionHeader(w, res.Title)
	PrintHistoryTable(w, view, res.Rows)
	fmt.Fprintf(w, "Page %d/%d (%d rows). %s\n", res.Page, res.TotalPages, res.TotalRows, res.FooterExtra)
}

// PrintHistoryTable writes ID, TITLE, URL, and WHEN/PLAYS columns.
func PrintHistoryTable(w io.Writer, view string, rows []music.HistoryCLIRow) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	col := "WHEN"
	if view == "counts" {
		col = "PLAYS"
	}
	fmt.Fprintf(tw, "ID\tTITLE\tURL\t%s\n", col)
	for _, r := range rows {
		fmt.Fprintf(tw, "%6d\t%s\t%s\t%s\n", r.ID, r.Title, r.URL, r.Tail)
	}
	_ = tw.Flush()
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
