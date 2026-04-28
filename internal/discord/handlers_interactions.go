package discord

import (
	"context"
	"log"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/commandkit"
	"github.com/keshon/melodix/internal/command"
	"github.com/keshon/melodix/internal/discord/respond"
)

// onInteractionCreate dispatches slash commands, context menu commands, and component interactions.
func (b *Bot) onInteractionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		b.onApplicationCommand(s, i)
	case discordgo.InteractionMessageComponent:
		b.onComponentInteraction(s, i)
	default:
		log.Printf("[DEBUG] Unhandled interaction type: %d", i.Type)
	}
}

func (b *Bot) onApplicationCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	name := i.ApplicationCommandData().Name
	c := commandkit.DefaultRegistry.Get(name)
	if c == nil {
		log.Printf("[WARN] Unknown command: %s", name)
		return
	}

	b.mu.RLock()
	logger := b.cmdLogger
	b.mu.RUnlock()

	var inv *commandkit.Invocation
	switch i.ApplicationCommandData().CommandType {
	case discordgo.MessageApplicationCommand:
		inv = &commandkit.Invocation{Data: &command.MessageApplicationCommandContext{
			Session: s, Event: i, Storage: b.storage, Target: i.Message,
			Config: b.cfg, Responder: respond.DefaultResponder, Logger: logger,
		}}
	case discordgo.ChatApplicationCommand:
		inv = &commandkit.Invocation{Data: &command.SlashInteractionContext{
			Session: s, Event: i, Storage: b.storage,
			Config: b.cfg, Responder: respond.DefaultResponder, Logger: logger,
			SystemBus: b.systemBus,
		}}
	default:
		return
	}

	b.runGuardedInteraction(s, i, "slash "+name, func(cmdCtx context.Context) error {
		return c.Run(cmdCtx, inv)
	})
}

func (b *Bot) onComponentInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID
	log.Printf("[DEBUG] Component interaction: %s", customID)

	var matched commandkit.Command
	for _, c := range commandkit.DefaultRegistry.GetAll() {
		if matchesComponentID(customID, c.Name()) {
			matched = c
			break
		}
	}
	if matched == nil {
		log.Printf("[WARN] No component handler for customID: %s", customID)
		return
	}

	handler, ok := commandkit.Root(matched).(command.ComponentInteractionHandler)
	if !ok {
		log.Printf("[WARN] Command %s does not implement ComponentInteractionHandler", matched.Name())
		return
	}

	b.mu.RLock()
	logger := b.cmdLogger
	b.mu.RUnlock()

	b.runGuardedInteraction(s, i, "component "+matched.Name(), func(cmdCtx context.Context) error {
		_ = cmdCtx
		return handler.Component(&command.ComponentInteractionContext{
			Session: s, Event: i, Storage: b.storage,
			Config: b.cfg, Responder: respond.DefaultResponder, Logger: logger,
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

