package cmdadapter

import (
	"context"

	"github.com/bwmarrin/discordgo"
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

func (a *Adapter) SlashDefinition() *discordgo.ApplicationCommand {
	if sp, ok := a.Cmd.(SlashProvider); ok {
		return sp.SlashDefinition()
	}
	return nil
}

func (a *Adapter) ContextDefinition() *discordgo.ApplicationCommand {
	if cp, ok := a.Cmd.(ContextMenuProvider); ok {
		return cp.ContextDefinition()
	}
	return nil
}

func (a *Adapter) ReactionDefinition() string {
	if rp, ok := a.Cmd.(ReactionProvider); ok {
		return rp.ReactionDefinition()
	}
	return ""
}

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
