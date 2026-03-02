package middleware

import (
	"context"
	"log"

	"github.com/keshon/commandkit"
	"github.com/keshon/melodix/internal/command"
	"github.com/keshon/melodix/internal/storage"

	"github.com/bwmarrin/discordgo"
)

// WithCommandLogger wraps a command to log its execution
func WithCommandLogger() commandkit.Middleware {
	return func(c commandkit.Command) commandkit.Command {
		return commandkit.Wrap(c, func(ctx context.Context, inv *commandkit.Invocation) error {
			err := c.Run(ctx, inv)

			logCmd := func(s *discordgo.Session, stor *storage.Storage, guildID, channelID, userID, username, cmdName string) {
				var logger command.CommandLogger
				switch v := inv.Data.(type) {
				case *command.SlashInteractionContext:
					logger = v.Logger
				case *command.ComponentInteractionContext:
					logger = v.Logger
				case *command.MessageApplicationCommandContext:
					logger = v.Logger
				case *command.MessageReactionContext:
					logger = v.Logger
				default:
					return
				}
				if logger != nil {
					if e := logger.LogCommand(s, stor, guildID, channelID, userID, username, cmdName); e != nil {
						log.Printf("[WARN] Failed to log command /%s: %v", cmdName, e)
					}
				}
			}

			switch v := inv.Data.(type) {
			case *command.SlashInteractionContext:
				e := v.Event
				user := resolveUser(v.Session, e)
				logCmd(v.Session, v.Storage, e.GuildID, e.ChannelID, user.ID, user.Username, c.Name())
			case *command.ComponentInteractionContext:
				e := v.Event
				user := resolveUser(v.Session, e)
				logCmd(v.Session, v.Storage, e.GuildID, e.ChannelID, user.ID, user.Username, c.Name())
			case *command.MessageApplicationCommandContext:
				e := v.Event
				user := resolveUser(v.Session, e)
				logCmd(v.Session, v.Storage, e.GuildID, e.ChannelID, user.ID, user.Username, c.Name())
			case *command.MessageContext:
				// skip message commands
			case *command.MessageReactionContext:
				if v.Storage != nil && v.Logger != nil {
					user := v.Event.UserID
					if e := v.Logger.LogCommand(v.Session, v.Storage, v.Event.GuildID, v.Event.ChannelID, user, user, c.Name()); e != nil {
						log.Printf("[WARN] Failed to log reaction /%s: %v", c.Name(), e)
					}
				}
			}
			return err
		})
	}
}

// resolveUser safely retrieves the user object from an InteractionCreate event
func resolveUser(s *discordgo.Session, e *discordgo.InteractionCreate) *discordgo.User {
	if e.Member != nil && e.Member.User != nil {
		return e.Member.User
	}
	if e.User != nil {
		return e.User
	}

	// As last resort, try fetching from Discord API
	if e.Member != nil && e.Member.User != nil {
		return e.Member.User
	}

	// If we know the user ID but not username — fetch it
	if e.User != nil {
		if u, err := s.User(e.User.ID); err == nil {
			return u
		}
	}
	// Safe fallback
	return &discordgo.User{ID: "unknown", Username: "Unknown"}
}
