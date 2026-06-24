package middleware

import (
	"context"

	"github.com/keshon/command"
	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/storage"

	"github.com/bwmarrin/discordgo"
)

// WithGroupAccessCheck wraps a command to enforce group access
func WithGroupAccessCheck() command.Middleware {
	return func(c command.Command) command.Command {
		return command.Wrap(c, func(ctx context.Context, inv *command.Invocation) error {
			var (
				guildID string
				stor    *storage.Storage
				respond func(string)
			)

			switch v := inv.Data.(type) {
			case *cmdadapter.SlashInteractionContext:
				guildID, stor = v.Event.GuildID, v.Storage
				if v.Responder != nil {
					respond = func(msg string) {
						_ = v.Responder.RespondEmbedEphemeral(v.Session, v.Event, &discordgo.MessageEmbed{Description: msg})
					}
				} else {
					respond = func(_ string) {}
				}
			case *cmdadapter.ComponentInteractionContext:
				guildID, stor = v.Event.GuildID, v.Storage
				if v.Responder != nil {
					respond = func(msg string) {
						_ = v.Responder.RespondEmbedEphemeral(v.Session, v.Event, &discordgo.MessageEmbed{Description: msg})
					}
				} else {
					respond = func(_ string) {}
				}
				if disabledGroup(c, guildID, stor, respond) {
					return nil
				}
				if ch, ok := command.Root(c).(cmdadapter.ComponentInteractionHandler); ok {
					return ch.Component(v)
				}
				return nil
			case *cmdadapter.MessageApplicationCommandContext:
				guildID, stor = v.Event.GuildID, v.Storage
				if v.Responder != nil {
					respond = func(msg string) {
						_ = v.Responder.RespondEmbedEphemeral(v.Session, v.Event, &discordgo.MessageEmbed{Description: msg})
					}
				} else {
					respond = func(_ string) {}
				}
			case *cmdadapter.MessageContext:
				guildID, stor = v.Event.GuildID, v.Storage
				respond = func(_ string) {}
			case *cmdadapter.MessageReactionContext:
				guildID, stor = v.Event.GuildID, v.Storage
				respond = func(_ string) {}
			default:
				return c.Run(ctx, inv)
			}

			if disabledGroup(c, guildID, stor, respond) {
				return nil
			}
			return c.Run(ctx, inv)
		})
	}
}

func disabledGroup(c command.Command, guildID string, stor *storage.Storage, respond func(string)) bool {
	meta, ok := command.Root(c).(cmdadapter.Meta)
	if !ok || meta.Group() == "" {
		return false
	}
	disabled, err := stor.IsGroupDisabled(guildID, meta.Group())
	if err != nil {
		return false
	}
	if disabled {
		respond("This command is disabled on this server.\nUse `/commands status` to check which commands are disabled.")
		return true
	}
	return false
}
