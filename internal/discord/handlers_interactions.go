package discord

import (
	"context"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/command"
	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/discord/discordreply"
)

// onInteractionCreate dispatches slash commands, context menu commands, and component interactions.
func (b *Bot) onInteractionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		b.onApplicationCommand(s, i)
	case discordgo.InteractionMessageComponent:
		b.onComponentInteraction(s, i)
	default:
		b.log.Debug().Int("interaction_type", int(i.Type)).Msg("interaction_unhandled")
	}
}

func (b *Bot) onApplicationCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	name := i.ApplicationCommandData().Name
	c := command.DefaultRegistry.Get(name)
	if c == nil {
		b.log.Warn().Str("command", name).Msg("command_unknown")
		return
	}

	b.mu.RLock()
	logger := b.cmdLogger
	b.mu.RUnlock()

	var inv *command.Invocation
	switch i.ApplicationCommandData().CommandType {
	case discordgo.MessageApplicationCommand:
		inv = &command.Invocation{Data: &cmdadapter.MessageApplicationCommandContext{
			Session: s, Event: i, Storage: b.storage, Target: i.Message,
			Config: b.cfg, Responder: discordreply.DefaultResponder, Logger: logger,
			AppLog: b.log,
		}}
	case discordgo.ChatApplicationCommand:
		inv = &command.Invocation{Data: &cmdadapter.SlashInteractionContext{
			Session: s, Event: i, Storage: b.storage,
			Config: b.cfg, Responder: discordreply.DefaultResponder, Logger: logger,
			AppLog: b.log,
			Syncer: b.cmdSyncer,
		}}
	default:
		return
	}

	b.runGuardedInteraction(s, i, "slash", name, func(cmdCtx context.Context) error {
		return c.Run(cmdCtx, inv)
	})
}

func (b *Bot) onComponentInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID
	b.log.Debug().Str("custom_id", customID).Msg("component_interaction")

	var matched command.Command
	for _, c := range command.DefaultRegistry.GetAll() {
		if matchesComponentID(customID, c.Name()) {
			matched = c
			break
		}
	}
	if matched == nil {
		b.log.Warn().Str("custom_id", customID).Msg("component_no_handler")
		return
	}

	handler, ok := command.Root(matched).(cmdadapter.ComponentInteractionHandler)
	if !ok {
		b.log.Warn().Str("command", matched.Name()).Msg("component_handler_missing")
		return
	}

	b.mu.RLock()
	logger := b.cmdLogger
	b.mu.RUnlock()

	b.runGuardedInteraction(s, i, "component", matched.Name(), func(cmdCtx context.Context) error {
		_ = cmdCtx
		return handler.Component(&cmdadapter.ComponentInteractionContext{
			Session: s, Event: i, Storage: b.storage,
			Config: b.cfg, Responder: discordreply.DefaultResponder, Logger: logger,
			AppLog: b.log,
		})
	})
}

// matchesComponentID reports whether a component customID belongs to a command.
// CustomIDs follow the convention "commandName", "commandName:...", or "commandName_...".
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
