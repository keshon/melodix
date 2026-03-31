package middleware

import (
	"context"

	"github.com/keshon/commandkit"
	"github.com/keshon/melodix/internal/command"
)

// WithGuildOnly wraps a command to enforce guild-only access
func WithGuildOnly() commandkit.Middleware {
	return func(c commandkit.Command) commandkit.Command {
		return commandkit.Wrap(c, func(ctx context.Context, inv *commandkit.Invocation) error {
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
