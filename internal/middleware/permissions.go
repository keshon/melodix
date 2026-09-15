package middleware

import (
	"context"
	"fmt"
	"strings"

	"github.com/keshon/command"
	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/discord/perm"
)

// WithUserPermissionCheck refuses a command the caller is not allowed to run,
// and names a permission that would have allowed it.
//
// The permission bits are Discord's, but nothing here needs to know that: the
// invocation reports the caller's effective bits, and perm turns a bit into
// the wording Discord's own UI uses.
func WithUserPermissionCheck() command.Middleware {
	return func(c command.Command) command.Command {
		return command.Wrap(c, func(ctx context.Context, inv *command.Invocation) error {
			cc := cmdadapter.ContextFromInvocation(inv)
			if cc == nil {
				return c.Run(ctx, inv)
			}
			// No guild means no roles to check against, and an unidentifiable
			// caller means the answer would be about nobody.
			if cc.GuildID() == "" || cc.UserID() == cmdadapter.UnknownUserID {
				return c.Run(ctx, inv)
			}

			memberPerms, err := cc.MemberPermissions()
			if err != nil {
				return fmt.Errorf("failed to get user permissions: %w", err)
			}
			if memberPerms&perm.Administrator != 0 {
				return c.Run(ctx, inv)
			}

			meta, ok := command.Root(c).(cmdadapter.Meta)
			if !ok {
				return c.Run(ctx, inv)
			}
			required := meta.UserPermissions()
			if len(required) == 0 {
				return c.Run(ctx, inv)
			}
			for _, p := range required {
				if memberPerms&p != 0 {
					return c.Run(ctx, inv)
				}
			}

			allowed := make([]string, 0, len(required))
			for _, p := range required {
				allowed = append(allowed, perm.Name(p))
			}
			_ = cc.ReplyEphemeral(fmt.Sprintf(
				"You need at least one of the following permissions to run this command:\n`%s`",
				strings.Join(allowed, "`, `"),
			))
			return nil
		})
	}
}
