package middleware

import (
	"context"
	"sync"
	"testing"

	"github.com/keshon/command"
	"github.com/rs/zerolog"

	"github.com/keshon/melodix/internal/discord/adapter"
	"github.com/keshon/melodix/internal/storage"
)

// buttonCommand is a command with a component handler, like /search.
type buttonCommand struct {
	fakeCommand
	clicks *int
}

func (b buttonCommand) Group() string { return "music" }

func (b buttonCommand) Component(*adapter.ComponentInteractionContext) error {
	*b.clicks++
	return nil
}

type recordingAudit struct {
	mu   sync.Mutex
	rows []string
}

func (a *recordingAudit) LogCommand(_, _, _, _, name string) error {
	a.mu.Lock()
	a.rows = append(a.rows, name)
	a.mu.Unlock()
	return nil
}

func fullChain() []command.Middleware {
	return []command.Middleware{
		WithGroupAccessCheck(),
		WithGuildOnly(),
		WithUserPermissionCheck(),
		WithCommandLogger(zerolog.Nop()),
	}
}

func clickThroughChain(t *testing.T, stor *storage.Storage, audit *recordingAudit) int {
	t.Helper()
	clicks := 0
	c := command.Apply(&adapter.Adapter{Cmd: buttonCommand{clicks: &clicks}}, fullChain()...)
	inv := &command.Invocation{Data: &adapter.ComponentInteractionContext{
		Invoker:     adapter.Invoker{GuildID: "g1", ChannelID: "c1", UserID: "u1"},
		ComponentID: "fake:1",
		Storage:     stor,
		Audit:       audit,
		AppLog:      zerolog.Nop(),
	}}
	if err := c.Run(context.Background(), inv); err != nil {
		t.Fatalf("run: %v", err)
	}
	return clicks
}

func newStorage(t *testing.T) *storage.Storage {
	t.Helper()
	stor, err := storage.NewStorage(t.TempDir(), zerolog.Nop())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	t.Cleanup(func() { _ = stor.Close() })
	return stor
}

// A button goes through the same middleware as the command that posted it.
// Clicks used to be dispatched straight to the handler, so they were never
// audited, and the group check's own component branch -- the one place meant
// to stop a click -- could not be reached.
func TestAClickIsAudited(t *testing.T) {
	audit := &recordingAudit{}
	if got := clickThroughChain(t, newStorage(t), audit); got != 1 {
		t.Fatalf("handler ran %d times, want 1", got)
	}
	audit.mu.Lock()
	defer audit.mu.Unlock()
	if len(audit.rows) != 1 {
		t.Fatalf("audited %d rows for one click, want 1", len(audit.rows))
	}
}

// Disabling a group has to disable its buttons too: a chooser posted before
// the admin switched music off would otherwise go on starting playback.
func TestAClickInADisabledGroupIsRefused(t *testing.T) {
	stor := newStorage(t)
	if err := stor.DisableGroup("g1", "music"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if got := clickThroughChain(t, stor, &recordingAudit{}); got != 0 {
		t.Fatalf("handler ran %d times in a disabled group, want 0", got)
	}
}
