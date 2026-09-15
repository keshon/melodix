package cmdadapter

import (
	"context"

	"github.com/keshon/command"
)

type Adapter struct {
	Cmd Handler
}

func (a *Adapter) Name() string             { return a.Cmd.Name() }
func (a *Adapter) Description() string      { return a.Cmd.Description() }
func (a *Adapter) Group() string            { return a.Cmd.Group() }
func (a *Adapter) Category() string         { return a.Cmd.Category() }
func (a *Adapter) UserPermissions() []int64 { return a.Cmd.UserPermissions() }

func (a *Adapter) Run(ctx context.Context, inv *command.Invocation) error {
	return a.Cmd.Run(inv.Data)
}

// SlashDefinition forwards the declaration in the form the interface declares
// it, so that the Adapter satisfies SlashProvider.
//
// It used to render the wire form here instead. Nothing type-checked that:
// middleware unwraps to the Adapter and cmdsync asks it for a SlashProvider,
// and an Adapter whose method returns a different type simply is not one. The
// assertion failed, every command resolved to no definition, and a sync that
// wants nothing deletes everything the guild has. Rendering belongs at the
// point of registration, which already does it.
func (a *Adapter) SlashDefinition() *SlashCommand {
	if sp, ok := a.Cmd.(SlashProvider); ok {
		return sp.SlashDefinition()
	}
	return nil
}

func (a *Adapter) ContextDefinition() *SlashCommand {
	if cp, ok := a.Cmd.(ContextMenuProvider); ok {
		return cp.ContextDefinition()
	}
	return nil
}

// Compile-time proof that the Adapter is what cmdsync looks for. Without
// these, the only thing standing between a changed signature and a guild
// losing all of its commands is a runtime type assertion that fails quietly.
var (
	_ SlashProvider       = (*Adapter)(nil)
	_ ContextMenuProvider = (*Adapter)(nil)
)

// SkipAuditLog reports whether the wrapped command opted out of the audit log.
//
// Middleware sees the Adapter, not the command inside it — command.Root unwraps
// to here and stops — so the opt-out has to be forwarded like every other
// optional capability rather than asserted through to a.Cmd from outside.
func (a *Adapter) SkipAuditLog() bool {
	_, ok := a.Cmd.(Unlogged)
	return ok
}

func (a *Adapter) Component(ctx *ComponentInteractionContext) error {
	if ch, ok := a.Cmd.(ComponentInteractionHandler); ok {
		return ch.Component(ctx)
	}
	return nil
}
