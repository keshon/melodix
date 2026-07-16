package discord

import (
	"context"
	"errors"
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/internal/discord/reply"
)

type commandRunOptions struct {
	onBusy    func(error)
	onTimeout func(error)
	onError   func(error)
}

func (b *Bot) runWithCommandContext(opts commandRunOptions, fn func(cmdCtx context.Context) error) {
	cmdCtx, cancel := b.commandContext()
	defer cancel()

	if err := b.acquireCommandSlot(cmdCtx); err != nil {
		if opts.onBusy != nil {
			opts.onBusy(err)
		}
		return
	}
	defer b.releaseCommandSlot()

	if err := fn(cmdCtx); err != nil {
		isTimeout := errors.Is(err, context.DeadlineExceeded) || errors.Is(cmdCtx.Err(), context.DeadlineExceeded)
		if isTimeout {
			if opts.onTimeout != nil {
				opts.onTimeout(err)
			}
			return
		}
		if opts.onError != nil {
			opts.onError(err)
		}
	}
}

// runGuardedInteraction runs a slash/component interaction under the bot's command guard.
// kind is the dispatch kind ("slash" or "component"); name is the resolved command name.
// Both end up as structured fields ("kind", "command") on every emitted log event.
func (b *Bot) runGuardedInteraction(
	s *discordgo.Session,
	i *discordgo.InteractionCreate,
	kind string,
	name string,
	fn func(cmdCtx context.Context) error,
) {
	b.runWithCommandContext(commandRunOptions{
		onBusy: func(err error) {
			b.log.Warn().Str("kind", kind).Str("command", name).Err(err).Msg("command_slot_busy")
			_ = reply.RespondEmbedEphemeral(s, i, &discordgo.MessageEmbed{
				Description: "Bot is busy right now. Please try again in a moment.",
			})
		},
		onTimeout: func(err error) {
			b.log.Warn().Str("kind", kind).Str("command", name).Err(err).Msg("command_timeout")
			_ = reply.RespondEmbedEphemeral(s, i, &discordgo.MessageEmbed{
				Description: "Timed out running command.",
			})
		},
		onError: func(err error) {
			b.log.Error().Str("kind", kind).Str("command", name).Err(err).Msg("command_run_error")
			_ = reply.RespondEmbedEphemeral(s, i, &discordgo.MessageEmbed{
				Description: fmt.Sprintf("Error running command: %v", err),
			})
		},
	}, fn)
}
