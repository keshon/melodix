package discord

import (
	"context"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/command"
	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/discord/reply"
)

// onInteractionCreate dispatches slash commands, context menu commands, and
// component interactions.
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

// interactionInvoker reads the caller off an interaction, once, so nothing
// downstream has to hold the interaction to ask again. A guild interaction
// carries Member, a direct message carries User, and neither is guaranteed.
func interactionInvoker(e *discordgo.InteractionCreate) cmdadapter.Invoker {
	who := cmdadapter.Invoker{
		UserID:   cmdadapter.UnknownUserID,
		Username: cmdadapter.UnknownUsername,
	}
	if e == nil {
		return who
	}
	who.GuildID = e.GuildID
	who.ChannelID = e.ChannelID

	u := e.User
	if e.Member != nil && e.Member.User != nil {
		u = e.Member.User
	}
	if u != nil {
		who.UserID = u.ID
		who.Username = u.Username
	}
	return who
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

	responder := reply.NewResponder(s, i)
	api := reply.NewSessionAPI(s)

	var inv *command.Invocation
	switch i.ApplicationCommandData().CommandType {
	case discordgo.MessageApplicationCommand:
		inv = &command.Invocation{Data: &cmdadapter.MessageApplicationCommandContext{
			Invoker: interactionInvoker(i), Responder: responder, API: api,
			Storage: b.storage, Config: b.cfg, Logger: logger, AppLog: b.log,
		}}
	case discordgo.ChatApplicationCommand:
		inv = &command.Invocation{Data: &cmdadapter.SlashInteractionContext{
			Invoker: interactionInvoker(i), Responder: responder, API: api,
			Arguments: reply.SlashArguments(i.ApplicationCommandData().Options),
			Storage:   b.storage, Config: b.cfg, Logger: logger, AppLog: b.log,
			Syncer: b.cmdSyncer,
		}}
	default:
		return
	}

	b.runGuardedInteraction(responder, "slash", name, func(cmdCtx context.Context) error {
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

	responder := reply.NewResponder(s, i)

	b.runGuardedInteraction(responder, "component", matched.Name(), func(cmdCtx context.Context) error {
		_ = cmdCtx
		return handler.Component(&cmdadapter.ComponentInteractionContext{
			Invoker: interactionInvoker(i), Responder: responder,
			API:         reply.NewSessionAPI(s),
			ComponentID: customID,
			Storage:     b.storage, Config: b.cfg, Logger: logger, AppLog: b.log,
		})
	})
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
