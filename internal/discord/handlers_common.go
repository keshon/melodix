package discord

import (
	"context"
	"errors"
	"fmt"

	"github.com/keshon/melodix/internal/discord/cmdadapter"
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

// runGuardedInteraction runs a slash/component interaction under the bot's
// command guard. kind is the dispatch kind ("slash" or "component"); name is
// the resolved command name. Both end up as structured fields ("kind",
// "command") on every emitted log event.
//
// It takes the responder the handler already built rather than the session and
// the event, because every refusal it can emit is an ephemeral reply to that
// one interaction — which is exactly what a responder is.
func (b *Bot) runGuardedInteraction(
	r cmdadapter.Responder,
	kind string,
	name string,
	fn func(cmdCtx context.Context) error,
) {
	refuse := func(msg string) {
		if r == nil {
			return
		}
		_ = r.RespondEmbed(&cmdadapter.Embed{Description: msg}, true)
	}

	b.runWithCommandContext(commandRunOptions{
		onBusy: func(err error) {
			b.log.Warn().Str("kind", kind).Str("command", name).Err(err).Msg("command_slot_busy")
			refuse("Bot is busy right now. Please try again in a moment.")
		},
		onTimeout: func(err error) {
			b.log.Warn().Str("kind", kind).Str("command", name).Err(err).Msg("command_timeout")
			refuse("Timed out running command.")
		},
		onError: func(err error) {
			b.log.Error().Str("kind", kind).Str("command", name).Err(err).Msg("command_run_error")
			refuse(fmt.Sprintf("Error running command: %v", err))
		},
	}, fn)
}

// matchesComponentID reports whether a component customID belongs to a command.
// CustomIDs follow the convention "commandName", "commandName:...", or
// "commandName_...".
func matchesComponentID(customID, commandName string) bool {
	if customID == commandName {
		return true
	}
	if len(customID) > len(commandName) {
		sep := customID[len(commandName)]
		return (sep == ':' || sep == '_') && customID[:len(commandName)] == commandName
	}
	return false
}
