package adapter

import (
	"context"
	"strings"
	"testing"

	"github.com/keshon/command"
)

type groupedCommand struct{}

func (groupedCommand) Name() string                                   { return "settings" }
func (groupedCommand) Description() string                            { return "Server settings" }
func (groupedCommand) Run(context.Context, *command.Invocation) error { return nil }
func (groupedCommand) SlashDefinition() *SlashCommand {
	return &SlashCommand{
		Name: "settings",
		Options: []SlashOption{{
			Name: "commands", Type: OptionSubCommandGroup,
			Options: []SlashOption{{Name: "log", Type: OptionSubCommand, Description: "Review recently used commands"}},
		}},
	}
}

var _ SlashProvider = groupedCommand{}

// The registry never holds a bare command: Register stores the result of
// command.Apply, a middleware wrapper with no SlashDefinition of its own. The
// flat help asserted SlashProvider on that wrapper, the assertion never
// matched, and /help flat listed no subcommand of any command for as long as
// middleware has existed. The category and group views already unwrapped with
// Root.
func TestSubcommandsAreListedForACommandBehindMiddleware(t *testing.T) {
	passthrough := func(next command.Command) command.Command {
		return command.Wrap(next, next.Run)
	}
	wrapped := command.Apply(groupedCommand{}, passthrough)

	got := FormatCommandWithSubcommands(wrapped)

	if !strings.Contains(got, "`/settings commands log`") {
		t.Fatalf("help for a wrapped command lists no subcommands:\n%s", got)
	}
}
