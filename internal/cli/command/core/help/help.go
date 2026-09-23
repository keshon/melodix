// Package help is the CLI's help: every command, grouped by category in the
// bot's order.
package help

import (
	"sort"
	"strings"

	"github.com/keshon/melodix/internal/cli"
	"github.com/keshon/melodix/internal/config"
)

type Command struct{}

func (Command) Name() string        { return "help" }
func (Command) Aliases() []string   { return []string{"?"} }
func (Command) Description() string { return "Get a list of available commands" }
func (Command) Category() string    { return "ℹ️ Information" }
func (Command) Usage() string       { return "" }

func (Command) Run(c *cli.Context, _ []string) error {
	cmds := c.Registry.All()
	sort.SliceStable(cmds, func(i, j int) bool {
		wi, wj := config.CategoryWeights[cmds[i].Category()], config.CategoryWeights[cmds[j].Category()]
		if wi != wj {
			return wi < wj
		}
		return cmds[i].Name() < cmds[j].Name()
	})

	category := ""
	for _, cmd := range cmds {
		if cmd.Category() != category {
			if category != "" {
				c.Println()
			}
			category = cmd.Category()
			c.Println(category)
		}
		synopsis := strings.TrimSpace(cmd.Name() + " " + cmd.Usage())
		c.Printf("  %-12s %s\n", cmd.Name(), cmd.Description())
		if synopsis != cmd.Name() {
			c.Printf("  %-12s %s\n", "", synopsis)
		}
	}
	c.Println()
	c.Println("quit leaves.")
	return nil
}
