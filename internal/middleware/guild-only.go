package middleware

import (
	"context"

	"github.com/keshon/melodix/internal/command"
	"github.com/keshon/melodix/pkg/cmd"
)

// WithGuildOnly wraps a command to enforce guild-only access
func WithGuildOnly() cmd.Middleware {
	return func(c cmd.Command) cmd.Command {
		return cmd.Wrap(c, func(ctx context.Context, inv *cmd.Invocation) error {
			if v, ok := inv.Data.(*command.SlashInteractionContext); ok && v.Event.GuildID == "" {
				return nil
			}
			if v, ok := inv.Data.(*command.MessageContext); ok && v.Event.GuildID == "" {
				return nil
			}
			return c.Run(ctx, inv)
		})
	}
}
