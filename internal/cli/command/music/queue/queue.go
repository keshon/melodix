// Package queue is the CLI's queue: what is playing and what is next.
package queue

import (
	"github.com/keshon/melodix/internal/cli"
	"github.com/keshon/melodix/internal/cli/command/music/tracklist"
	"github.com/keshon/melodix/pkg/music/parsers"
)

type Command struct{}

func (Command) Name() string        { return "queue" }
func (Command) Aliases() []string   { return nil }
func (Command) Description() string { return "Show what is playing and what is queued next" }
func (Command) Category() string    { return "🎵 Music" }
func (Command) Usage() string       { return "" }

func (Command) Run(c *cli.Context, _ []string) error {
	p := c.Player()
	var current *parsers.Track
	if t, ok := p.CurrentTrack(); ok {
		current = &t
	}
	c.Println(tracklist.Queue(current, p.Queue()))
	return nil
}
