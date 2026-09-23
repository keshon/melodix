// Package stop is the CLI's stop: stop playback and clear the queue.
package stop

import (
	"github.com/keshon/melodix/internal/cli"
)

type Command struct{}

func (Command) Name() string        { return "stop" }
func (Command) Aliases() []string   { return []string{"s"} }
func (Command) Description() string { return "Stop playback and clear queue" }
func (Command) Category() string    { return "🎵 Music" }
func (Command) Usage() string       { return "" }

func (Command) Run(c *cli.Context, _ []string) error {
	if err := c.Player().Stop(true); err != nil {
		return err
	}
	c.Println("⏹ Playback stopped. Queue cleared.")
	return nil
}
