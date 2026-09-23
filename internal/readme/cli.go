package readme

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/keshon/melodix/internal/cli"
)

// cliEntries lists the terminal's commands, each with its synopsis and
// aliases.
func cliEntries(reg *cli.Registry) []entry {
	var out []entry
	for _, c := range reg.All() {
		out = append(out, entry{
			name:     c.Name(),
			category: c.Category(),
			render:   func(buf *bytes.Buffer) { renderCLICommand(buf, c) },
		})
	}
	return out
}

func renderCLICommand(buf *bytes.Buffer, c cli.Command) {
	synopsis := c.Name()
	if u := strings.TrimSpace(c.Usage()); u != "" {
		synopsis += " " + u
	}
	fmt.Fprintf(buf, "- **`%s`** — %s", synopsis, c.Description())
	if aliases := c.Aliases(); len(aliases) > 0 {
		fmt.Fprintf(buf, " (also `%s`)", strings.Join(aliases, "`, `"))
	}
	buf.WriteString("\n")
}
