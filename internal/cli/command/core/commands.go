package core

import (
	"context"
	"fmt"
	"io"

	clicommand "github.com/keshon/melodix/internal/cli/command"
	"github.com/keshon/commandkit"
)

// PrintHelpDetail prints expanded usage for play and history.
func PrintHelpDetail(w io.Writer) {
	fmt.Fprintln(w, "play:")
	fmt.Fprintln(w, "  play <url|query|path> [source] [parser]")
	fmt.Fprintln(w, "    Enqueue a URL, search query, or local path. Optional source and parser override defaults.")
	fmt.Fprintln(w, "  play <id> <id> ...")
	fmt.Fprintln(w, "    Enqueue tracks from history by numeric id (space-separated ids).")
	fmt.Fprintln(w, "history:")
	fmt.Fprintln(w, "  history [timeline|counts] [page]")
	fmt.Fprintln(w, "    timeline - chronological plays (default). counts - grouped by URL with play counts.")
	fmt.Fprintln(w, "    page is 1-based.")
}

// Register adds core REPL verbs (help) to the registry.
func Register(reg *commandkit.Registry) {
	for _, c := range []clicommand.CLICommand{
		cmdHelp{name: "help", desc: "detailed usage"},
		cmdHelp{name: "?", desc: "detailed usage (short)"},
	} {
		clicommand.Register(reg, c)
	}
}

type cmdHelp struct {
	name, desc string
}

func (c cmdHelp) Name() string        { return c.name }
func (c cmdHelp) Description() string { return c.desc }

func (c cmdHelp) Run(ctx context.Context, d *clicommand.Data, args []string) error {
	PrintHelpDetail(d.Out)
	return nil
}
