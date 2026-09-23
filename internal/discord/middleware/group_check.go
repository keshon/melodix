package middleware

import (
	"context"

	"github.com/keshon/command"
	"github.com/keshon/melodix/internal/discord/adapter"
	"github.com/keshon/melodix/internal/storage"
)

// WithGroupAccessCheck stops a command whose group an admin has switched off.
//
// The refusal is only ever sent where it can be sent privately. A command
// group is usually disabled to keep it out of a channel, and announcing the
// refusal publicly every time somebody trips over it would put back exactly
// the noise the admin was removing.
func WithGroupAccessCheck() command.Middleware {
	return func(c command.Command) command.Command {
		return command.Wrap(c, func(ctx context.Context, inv *command.Invocation) error {
			cc := adapter.ContextFromInvocation(inv)
			if cc == nil {
				return c.Run(ctx, inv)
			}

			respond := func(string) {}
			if cc.CanReplyPrivately() {
				respond = func(msg string) { _ = cc.ReplyEphemeral(msg) }
			}

			// Slash commands and component clicks alike: a click carries on
			// down the chain to the rest of the middleware and the audit log,
			// and the Adapter at the bottom hands it to the component handler.
			if disabledGroup(c, cc.GuildID(), cc.Store(), respond) {
				return nil
			}
			return c.Run(ctx, inv)
		})
	}
}

func disabledGroup(c command.Command, guildID string, stor *storage.Storage, respond func(string)) bool {
	meta, ok := command.Root(c).(adapter.Meta)
	if !ok || meta.Group() == "" {
		return false
	}
	disabled, err := stor.IsGroupDisabled(guildID, meta.Group())
	if err != nil {
		return false
	}
	if disabled {
		respond("This command is disabled on this server.\nUse `/settings commands status` to check which commands are disabled.")
		return true
	}
	return false
}
