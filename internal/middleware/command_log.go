package middleware

import (
	"context"

	"github.com/keshon/command"
	"github.com/keshon/melodix/internal/discord/adapter"
	"github.com/rs/zerolog"
)

// auditSkipper is satisfied by adapter.Adapter, which forwards the opt-out
// declared by the command it wraps (see adapter.Unlogged).
type auditSkipper interface {
	SkipAuditLog() bool
}

// WithCommandLogger wraps a command to log its execution after Run completes.
// Logging is best-effort: failures are warned but never affect the command
// result.
//
// A command that opts out is returned unwrapped rather than wrapped and then
// filtered: there is then no code path on which its caller could be written to
// storage, and no wrong branch to edit later.
func WithCommandLogger(log zerolog.Logger) command.Middleware {
	return func(c command.Command) command.Command {
		if s, ok := command.Root(c).(auditSkipper); ok && s.SkipAuditLog() {
			return c
		}
		return command.Wrap(c, func(ctx context.Context, inv *command.Invocation) error {
			err := c.Run(ctx, inv)
			logInvocation(log, c.Name(), inv)
			return err
		})
	}
}

// logInvocation asks the invocation who ran it and hands that to the logger it
// carries. A context with no logger is not audited, which is how message
// commands stay out of the log.
func logInvocation(log zerolog.Logger, cmdName string, inv *command.Invocation) {
	cc := adapter.ContextFromInvocation(inv)
	if cc == nil {
		return
	}
	logger := cc.AuditLog()
	if logger == nil {
		return
	}
	if err := logger.LogCommand(cc.GuildID(), cc.ChannelID(), cc.UserID(), cc.Username(), cmdName); err != nil {
		log.Warn().Str("command", cmdName).Err(err).Msg("command_audit_write_failed")
	}
}
