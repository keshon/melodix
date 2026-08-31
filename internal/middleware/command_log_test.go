package middleware

import (
	"testing"

	"github.com/keshon/command"
	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/rs/zerolog"
)

// fakeCommand is the minimum a cmdadapter.Handler needs. The opt-out is tested
// against a fake rather than a real command because whether any particular
// command wants it is a product decision, while the mechanism has to work
// regardless — and a test bound to one command would go quiet the day that
// command changed its mind.
type fakeCommand struct{}

func (fakeCommand) Name() string             { return "fake" }
func (fakeCommand) Description() string      { return "a stand-in command" }
func (fakeCommand) Group() string            { return "test" }
func (fakeCommand) Category() string         { return "test" }
func (fakeCommand) UserPermissions() []int64 { return nil }
func (fakeCommand) Run(any) error            { return nil }

// unloggedCommand opts out; ordinaryCommand is the control.
type unloggedCommand struct{ fakeCommand }

func (unloggedCommand) Unlogged() {}

type ordinaryCommand struct{ fakeCommand }

// A command that declares cmdadapter.Unlogged must come back unwrapped, because
// wrapping is the only thing that would write its caller to storage. The
// guarantee is worth exactly as much as this test: without it, adding a
// middleware or renaming SkipAuditLog would quietly start recording who ran the
// command, and nothing user-visible would change until someone read the log.
func TestCommandLoggerSkipsUnloggedCommands(t *testing.T) {
	mw := WithCommandLogger(zerolog.Nop())

	got := command.Apply(&cmdadapter.Adapter{Cmd: unloggedCommand{}}, mw)
	if _, wrapped := got.(command.Unwrappable); wrapped {
		t.Fatal("an Unlogged command was wrapped by the audit logger: its caller would reach storage")
	}
}

// The control: without the opt-out the logger must still wrap, or the test
// above would pass just as happily against a middleware that logs nothing at
// all.
func TestCommandLoggerWrapsOrdinaryCommands(t *testing.T) {
	mw := WithCommandLogger(zerolog.Nop())

	got := command.Apply(&cmdadapter.Adapter{Cmd: ordinaryCommand{}}, mw)
	if _, wrapped := got.(command.Unwrappable); !wrapped {
		t.Fatal("an ordinary command was not wrapped: nothing would be logged for any command")
	}
}

// The opt-out has to survive the full chain, not just a lone middleware:
// command.Root unwraps to the Adapter, and that is what the check reads.
func TestUnloggedSurvivesTheFullMiddlewareChain(t *testing.T) {
	chain := []command.Middleware{
		WithGroupAccessCheck(),
		WithGuildOnly(),
		WithUserPermissionCheck(),
		WithCommandLogger(zerolog.Nop()),
	}

	got := command.Apply(&cmdadapter.Adapter{Cmd: unloggedCommand{}}, chain...)

	root, ok := command.Root(got).(*cmdadapter.Adapter)
	if !ok {
		t.Fatalf("command.Root returned %T, not the Adapter the opt-out is read from", command.Root(got))
	}
	if !root.SkipAuditLog() {
		t.Fatal("the opt-out was lost somewhere in the middleware chain")
	}
}
