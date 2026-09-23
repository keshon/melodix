// Package search is the CLI's search: a numbered list of hits to pick one
// from, where the bot offers buttons.
package search

import (
	"errors"
	"strconv"
	"strings"

	"github.com/keshon/melodix/internal/cli"
	"github.com/keshon/melodix/internal/cli/command/music/playback"
	"github.com/keshon/melodix/internal/cli/command/music/tracklist"
	"github.com/keshon/melodix/internal/music"
)

// resultCount is how many hits are offered, as many as the bot's chooser.
const resultCount = 5

type Command struct{}

func (Command) Name() string        { return "search" }
func (Command) Aliases() []string   { return nil }
func (Command) Description() string { return "Search and pick a track to play" }
func (Command) Category() string    { return "🎵 Music" }
func (Command) Usage() string       { return "<query> [source=youtube|soundcloud]" }

func (Command) Run(c *cli.Context, args []string) error {
	words, opts := cli.Options(args, "source")
	query := strings.TrimSpace(strings.Join(words, " "))
	if query == "" {
		c.Println("Usage: search", Command{}.Usage())
		return nil
	}

	hits, err := c.Music.Search(opts["source"], query, resultCount)
	if errors.Is(err, music.ErrNotSearchable) {
		c.Println(opts["source"], "cannot be searched.")
		return nil
	}
	if err != nil || len(hits) == 0 {
		c.Printf("Nothing found for %q.\n", query)
		return nil
	}
	for i, h := range hits {
		c.Println(tracklist.SearchLine(i+1, h.Title, h.URL, h.Author, h.Duration))
	}

	answer, ok := c.Ask("Pick a number (Enter to cancel): ")
	if !ok || answer == "" {
		return nil
	}
	n, err := strconv.Atoi(answer)
	if err != nil || n < 1 || n > len(hits) {
		c.Printf("%q is not one of the results.\n", answer)
		return nil
	}

	in := music.Input{Kind: music.InputQuery, Query: hits[n-1].URL}
	added, err := c.Music.Add(c.Scope, in, "", "")
	if err != nil {
		playback.AddError(c, err)
		return nil
	}
	playback.Start(c, added)
	return nil
}
