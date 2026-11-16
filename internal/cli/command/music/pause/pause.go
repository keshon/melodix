// Package pause pauses the CLI's current track.
package pause

import "github.com/keshon/melodix/internal/cli"

type Command struct{}

func (Command) Name() string        { return "pause" }
func (Command) Aliases() []string   { return nil }
func (Command) Description() string { return "Pause the current track" }
func (Command) Category() string    { return "🎵 Music" }
func (Command) Usage() string       { return "" }

func (Command) Run(c *cli.Context, _ []string) error {
	err := c.Player().Pause()
	if err != nil {
		return err
	}
	c.Println("⏸ Playback paused.")
	return nil
}
