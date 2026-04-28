package discord

import (
	"context"
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/commandkit"
	"github.com/keshon/melodix/internal/command"
	"github.com/keshon/melodix/internal/discord/respond"
)

// onMessageCreate handles @mention messages directed at the bot.
func (b *Bot) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}
	mentioned := false
	for _, u := range m.Mentions {
		if u.ID == s.State.User.ID {
			mentioned = true
			break
		}
	}
	if !mentioned {
		return
	}

	b.runWithCommandContext(commandRunOptions{
		onBusy: func(err error) {
			b.log.Warn().Str("kind", "message").Err(err).Msg("command_slot_busy")
		},
	}, func(cmdCtx context.Context) error {
		inv := &commandkit.Invocation{Data: &command.MessageContext{Session: s, Event: m, Storage: b.storage, Config: b.cfg}}
		for _, c := range commandkit.DefaultRegistry.GetAll() {
			if err := c.Run(cmdCtx, inv); err != nil {
				if cmdCtx.Err() == context.DeadlineExceeded {
					b.log.Warn().Str("kind", "message").Err(err).Msg("command_timeout")
					_ = respond.MessageEmbed(s, m.ChannelID, &discordgo.MessageEmbed{
						Description: "Timed out running command.",
					})
					continue
				}
				b.log.Error().Str("kind", "message").Err(err).Msg("command_run_error")
				_ = respond.MessageEmbed(s, m.ChannelID, &discordgo.MessageEmbed{
					Description: fmt.Sprintf("Error: %v", err),
				})
			}
		}
		return nil
	})
}

// onMessageReactionAdd handles reaction events for commands that use reactions.
func (b *Bot) onMessageReactionAdd(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
	b.mu.RLock()
	logger := b.cmdLogger
	b.mu.RUnlock()

	b.runWithCommandContext(commandRunOptions{
		onBusy: func(err error) {
			b.log.Warn().Str("kind", "reaction").Err(err).Msg("command_slot_busy")
		},
	}, func(cmdCtx context.Context) error {
		inv := &commandkit.Invocation{Data: &command.MessageReactionContext{
			Session: s, Event: r, Storage: b.storage, Config: b.cfg, Logger: logger,
		}}
		for _, c := range commandkit.DefaultRegistry.GetAll() {
			if _, ok := commandkit.Root(c).(command.ReactionProvider); !ok {
				continue
			}
			if err := c.Run(cmdCtx, inv); err != nil {
				if cmdCtx.Err() == context.DeadlineExceeded {
					b.log.Warn().Str("kind", "reaction").Err(err).Msg("command_timeout")
					continue
				}
				b.log.Error().Str("kind", "reaction").Err(err).Msg("command_run_error")
				_ = respond.MessageEmbed(s, r.ChannelID, &discordgo.MessageEmbed{
					Description: fmt.Sprintf("Error: %v", err),
				})
			}
		}
		return nil
	})
}

