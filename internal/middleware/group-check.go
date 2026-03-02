package middleware

import (
	"context"

	"github.com/keshon/melodix/internal/command"
	"github.com/keshon/melodix/internal/storage"
	"github.com/keshon/melodix/pkg/commandkit"

	"github.com/bwmarrin/discordgo"
)

// WithGroupAccessCheck wraps a command to enforce group access
func WithGroupAccessCheck() commandkit.Middleware {
	return func(c commandkit.Command) commandkit.Command {
		return commandkit.Wrap(c, func(ctx context.Context, inv *commandkit.Invocation) error {
			var (
				guildID string
				stor    *storage.Storage
				respond func(string)
			)

			switch v := inv.Data.(type) {
			case *command.SlashInteractionContext:
				guildID, stor = v.Event.GuildID, v.Storage
				if v.Responder != nil {
					respond = func(msg string) {
						_ = v.Responder.RespondEmbedEphemeral(v.Session, v.Event, &discordgo.MessageEmbed{Description: msg})
					}
				} else {
					respond = func(_ string) {}
				}
			case *command.ComponentInteractionContext:
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
				if ch, ok := commandkit.Root(c).(command.ComponentInteractionHandler); ok {
					return ch.Component(v)
				}
				return nil
			case *command.MessageApplicationCommandContext:
				guildID, stor = v.Event.GuildID, v.Storage
				if v.Responder != nil {
					respond = func(msg string) {
						_ = v.Responder.RespondEmbedEphemeral(v.Session, v.Event, &discordgo.MessageEmbed{Description: msg})
					}
				} else {
					respond = func(_ string) {}
				}
			case *command.MessageContext:
				guildID, stor = v.Event.GuildID, v.Storage
				respond = func(_ string) {}
			case *command.MessageReactionContext:
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

func disabledGroup(c commandkit.Command, guildID string, stor *storage.Storage, respond func(string)) bool {
	meta, ok := commandkit.Root(c).(command.DiscordMeta)
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
