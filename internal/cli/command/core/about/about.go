// Package about is the CLI's about: where this build came from.
package about

import (
	"runtime"
	"runtime/debug"

	"github.com/keshon/melodix/internal/cli"
)

type Command struct{}

func (Command) Name() string        { return "about" }
func (Command) Aliases() []string   { return nil }
func (Command) Description() string { return "Discover the origin of this bot" }
func (Command) Category() string    { return "ℹ️ Information" }
func (Command) Usage() string       { return "" }

func (Command) Run(c *cli.Context, _ []string) error {
	c.Println("Melodix — music player for Discord and the terminal.")
	c.Println("Repository: https://github.com/keshon/melodix")
	c.Println("Runtime:   ", runtime.Version())

	build, ok := debug.ReadBuildInfo()
	if ok {
		for _, setting := range build.Settings {
			if setting.Key == "vcs.revision" && setting.Value != "" {
				c.Println("Commit:    ", setting.Value)
				break
			}
		}
	}
	return nil
}
