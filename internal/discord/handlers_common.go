package discord

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/internal/discord/respond"
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

func (b *Bot) runGuardedInteraction(
	s *discordgo.Session,
	i *discordgo.InteractionCreate,
	label string,
	fn func(cmdCtx context.Context) error,
) {
	b.runWithCommandContext(commandRunOptions{
		onBusy: func(err error) {
			log.Printf("[WARN] Command slot acquire failed (%s): %v", label, err)
			_ = respond.RespondEmbedEphemeral(s, i, &discordgo.MessageEmbed{
				Description: "Bot is busy right now. Please try again in a moment.",
			})
		},
		onTimeout: func(err error) {
			log.Printf("[WARN] Command timed out (%s): %v", label, err)
			_ = respond.RespondEmbedEphemeral(s, i, &discordgo.MessageEmbed{
				Description: "Timed out running command.",
			})
		},
		onError: func(err error) {
			log.Printf("[ERR] Error running command %s: %v", label, err)
			_ = respond.RespondEmbedEphemeral(s, i, &discordgo.MessageEmbed{
				Description: fmt.Sprintf("Error running command: %v", err),
			})
		},
	}, fn)
}

