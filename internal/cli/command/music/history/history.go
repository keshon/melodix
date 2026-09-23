// Package history is the CLI's history: recently played tracks, to replay by
// id with play.
package history

import (
	"errors"
	"strconv"

	"github.com/keshon/melodix/internal/cli"
	"github.com/keshon/melodix/internal/cli/command/music/tracklist"
	"github.com/keshon/melodix/internal/music"
)

// linesPerPage matches the bot's history pages.
const linesPerPage = 15

type Command struct{}

func (Command) Name() string      { return "history" }
func (Command) Aliases() []string { return nil }
func (Command) Description() string {
	return "Show recently played tracks (replay by id with play)"
}
func (Command) Category() string { return "🎵 Music" }
func (Command) Usage() string    { return "[timeline|counts] [page]" }

func (Command) Run(c *cli.Context, args []string) error {
	view, page := "timeline", 1
	for _, a := range args {
		if n, err := strconv.Atoi(a); err == nil {
			page = n
			continue
		}
		view = a
	}

	rows, err := c.Music.History(c.Scope)
	if errors.Is(err, music.ErrHistoryUnavailable) {
		c.Println("Playback history is not available: the bot holds the data directory, or storage failed to open.")
		return nil
	}
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		c.Println("No playback history yet. Use play first.")
		return nil
	}

	var lines []string
	switch view {
	case "counts":
		for _, r := range music.PlayCounts(rows) {
			lines = append(lines, tracklist.CountsLine(r.ID, r.Title, r.URL, r.Count))
		}
	case "timeline":
		for _, r := range rows {
			lines = append(lines, tracklist.TimelineLine(r.ID, r.Title, r.URL, r.PlayedAt))
		}
	default:
		c.Println("Usage: history", Command{}.Usage())
		return nil
	}

	pages := (len(lines) + linesPerPage - 1) / linesPerPage
	page = min(max(page, 1), pages)
	start := (page - 1) * linesPerPage
	for _, line := range lines[start:min(start+linesPerPage, len(lines))] {
		c.Println(line)
	}
	c.Printf("Page %d/%d (%d rows). Replay with play <id>.\n", page, pages, len(lines))
	return nil
}
