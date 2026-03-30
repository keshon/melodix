package command

import (
	"context"

	"github.com/keshon/commandkit"
)

// CLICommand is a REPL verb (name + description + run). Run receives parsed args after the command name.
type CLICommand interface {
	Name() string
	Description() string
	Run(ctx context.Context, d *Data, args []string) error
}

// CLIAdapter implements commandkit.Command by wrapping CLICommand and passing inv.Data as *Data.
type CLIAdapter struct {
	Cmd CLICommand
}

func (a *CLIAdapter) Name() string        { return a.Cmd.Name() }
func (a *CLIAdapter) Description() string { return a.Cmd.Description() }

func (a *CLIAdapter) Run(ctx context.Context, inv *commandkit.Invocation) error {
	d, ok := inv.Data.(*Data)
	if !ok || d == nil {
		return nil
	}
	return a.Cmd.Run(ctx, d, inv.Args)
}

// Register adds a CLI command to the given registry (not the Discord DefaultRegistry).
func Register(reg *commandkit.Registry, cmd CLICommand, mws ...commandkit.Middleware) {
	c := commandkit.Apply(&CLIAdapter{Cmd: cmd}, mws...)
	reg.Register(c)
}
