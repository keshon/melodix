// Package next is the CLI's next: skip to the next queued track.
package next

import (
	"github.com/keshon/melodix/internal/cli"
)

type Command struct{}

func (Command) Name() string        { return "next" }
func (Command) Aliases() []string   { return []string{"n", "skip"} }
func (Command) Description() string { return "Skip to the next track" }
func (Command) Category() string    { return "🎵 Music" }
func (Command) Usage() string       { return "" }

func (Command) Run(c *cli.Context, _ []string) error {
	p := c.Player()
	if len(p.Queue()) == 0 {
		c.Println("No tracks left to skip.")
		return nil
	}
	if skipped, ok := p.CurrentTrack(); ok {
		c.Println("⏭ Skipped:", skipped.Title)
	}
	_ = p.Stop(false)
	if err := p.PlayNext(""); err != nil {
		c.Println("Failed to play next track:", err)
	}
	return nil
}
