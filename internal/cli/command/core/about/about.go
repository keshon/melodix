// Package about is the CLI's about: where this build came from.
package about

import (
	"github.com/keshon/buildinfo"

	"github.com/keshon/melodix/internal/cli"
)

type Command struct{}

func (Command) Name() string        { return "about" }
func (Command) Aliases() []string   { return nil }
func (Command) Description() string { return "Discover the origin of this bot" }
func (Command) Category() string    { return "ℹ️ Information" }
func (Command) Usage() string       { return "" }

func (Command) Run(c *cli.Context, _ []string) error {
	info := buildinfo.Get()
	c.Println(info.Project, "—", info.Description)
	c.Println("Repository: https://github.com/keshon/melodix")
	c.Println("Commit:    ", info.Commit)
	c.Println("Release:   ", info.BuildTime, "("+info.GoVersion+")")
	return nil
}
