// Package resume resumes the CLI's current track.
package resume

import "github.com/keshon/melodix/internal/cli"

type Command struct{}

func (Command) Name() string        { return "resume" }
func (Command) Aliases() []string   { return nil }
func (Command) Description() string { return "Resume the current track" }
func (Command) Category() string    { return "🎵 Music" }
func (Command) Usage() string       { return "" }

func (Command) Run(c *cli.Context, _ []string) error {
	err := c.Player().Resume()
	if err != nil {
		return err
	}
	c.Println("▶ Playback resumed.")
	return nil
}
