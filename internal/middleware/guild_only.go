package middleware

import (
	"context"

	"github.com/keshon/command"
	"github.com/keshon/melodix/internal/discord/cmdadapter"
)

// WithGuildOnly wraps a command to enforce guild-only access
func WithGuildOnly() command.Middleware {
	return func(c command.Command) command.Command {
		return command.Wrap(c, func(ctx context.Context, inv *command.Invocation) error {
			// Asking the context rather than type-switching over two of the
			// five also closes the gap that the switch left: a component
			// interaction or a context-menu command in a direct message used
			// to reach a guild-only command, because neither type was listed.
			if cc := cmdadapter.ContextFromInvocation(inv); cc != nil && cc.GuildID() == "" {
				return nil
			}
			return c.Run(ctx, inv)
		})
	}
}
