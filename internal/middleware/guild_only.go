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
			if v, ok := inv.Data.(*cmdadapter.SlashInteractionContext); ok && v.Event.GuildID == "" {
				return nil
			}
			if v, ok := inv.Data.(*cmdadapter.MessageContext); ok && v.Event.GuildID == "" {
				return nil
			}
			return c.Run(ctx, inv)
		})
	}
}
