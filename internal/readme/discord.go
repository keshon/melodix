package readme

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/keshon/command"

	"github.com/keshon/melodix/internal/discord/adapter"
)

// discordEntries lists the bot's commands, each with its slash subcommands.
func discordEntries(reg *command.Registry) []entry {
	var out []entry
	for _, c := range reg.GetAll() {
		root := command.Root(c)
		cat := ""
		if meta, ok := root.(adapter.Meta); ok {
			cat = meta.Category()
		}
		out = append(out, entry{
			name:     c.Name(),
			category: cat,
			render:   func(buf *bytes.Buffer) { renderDiscordCommand(buf, root) },
		})
	}
	return out
}

func renderDiscordCommand(buf *bytes.Buffer, c command.Command) {
	name := c.Name()
	display := name
	if !hasSpace(name) && !startsWithUpper(name) {
		display = "/" + display
	}

	fmt.Fprintf(buf, "- **%s** — %s\n", display, c.Description())

	sp, ok := c.(adapter.SlashProvider)
	if !ok {
		return
	}

	def := sp.SlashDefinition()
	if def == nil {
		return
	}

	var sub strings.Builder
	adapter.AppendSlashSubcommands(&sub, def.Name, def.Options, "")
	for _, line := range strings.Split(sub.String(), "\n") {
		// Lines look like:  `/help category` - description
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimPrefix(line, "`")
		parts := strings.SplitN(line, "` - ", 2)
		if len(parts) == 2 {
			fmt.Fprintf(buf, "  - **%s** — %s\n", parts[0], parts[1])
		}
	}
}

func hasSpace(s string) bool {
	for _, r := range s {
		if r == ' ' {
			return true
		}
	}
	return false
}

func startsWithUpper(s string) bool {
	if s == "" {
		return false
	}
	r := rune(s[0])
	return r >= 'A' && r <= 'Z'
}
