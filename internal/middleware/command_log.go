// FILE: melodix/internal/discord/middleware/command_logger.go
package middleware

import (
	"context"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/command"
	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/rs/zerolog"
)

// WithCommandLogger wraps a command to log its execution after Run completes.
// Logging is best-effort: failures are warned but never affect the command result.
func WithCommandLogger(log zerolog.Logger) command.Middleware {
	return func(c command.Command) command.Command {
		return command.Wrap(c, func(ctx context.Context, inv *command.Invocation) error {
			err := c.Run(ctx, inv)
			logInvocation(log, c.Name(), inv)
			return err
		})
	}
}

// logInvocation resolves the invocation context and delegates to the injected logger.
func logInvocation(log zerolog.Logger, cmdName string, inv *command.Invocation) {
	switch v := inv.Data.(type) {
	case *cmdadapter.SlashInteractionContext:
		logInteraction(log, cmdName, v.Logger, v.Session, v.Event)

	case *cmdadapter.ComponentInteractionContext:
		logInteraction(log, cmdName, v.Logger, v.Session, v.Event)

	case *cmdadapter.MessageApplicationCommandContext:
		logInteraction(log, cmdName, v.Logger, v.Session, v.Event)

	case *cmdadapter.MessageReactionContext:
		if v.Logger != nil {
			logEntry(log, cmdName, v.Logger, v.Event.GuildID, v.Event.ChannelID, v.Event.UserID, v.Event.UserID)
		}

	case *cmdadapter.MessageContext:
		// Message commands are intentionally not logged.

	default:
		// Unknown context type — nothing to log.
	}
}

// logInteraction extracts user info from an InteractionCreate event and logs it.
func logInteraction(log zerolog.Logger, cmdName string, logger cmdadapter.Logger, s *discordgo.Session, e *discordgo.InteractionCreate) {
	if logger == nil {
		return
	}
	user := resolveUser(s, e)
	logEntry(log, cmdName, logger, e.GuildID, e.ChannelID, user.ID, user.Username)
}

// logEntry calls the logger and warns on failure.
func logEntry(log zerolog.Logger, cmdName string, logger cmdadapter.Logger, guildID, channelID, userID, username string) {
	if err := logger.LogCommand(guildID, channelID, userID, username, cmdName); err != nil {
		log.Warn().Str("command", cmdName).Err(err).Msg("command_audit_write_failed")
	}
}

// resolveUser returns the User from an InteractionCreate, trying Member first,
// then User, and falling back to a safe sentinel value if neither is present.
func resolveUser(s *discordgo.Session, e *discordgo.InteractionCreate) *discordgo.User {
	if e.Member != nil && e.Member.User != nil {
		return e.Member.User
	}
	if e.User != nil {
		return e.User
	}
	// Last resort: fetch from Discord API by user ID.
	if e.User != nil {
		if u, err := s.User(e.User.ID); err == nil {
			return u
		}
	}
	return &discordgo.User{ID: "unknown", Username: "Unknown"}
}
