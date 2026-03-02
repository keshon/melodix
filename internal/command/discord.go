package command

import (
	"context"

	"github.com/keshon/commandkit"
	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/storage"

	"github.com/bwmarrin/discordgo"
)

// Responder is used by commands to reply without importing the discord package (avoids import cycles).
type Responder interface {
	RespondEmbedEphemeral(s *discordgo.Session, e *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) error
	RespondEmbed(s *discordgo.Session, e *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) error
	CheckBotPermissions(s *discordgo.Session, channelID string) bool
	EmbedColor() int
}

// CommandLogger logs command execution (avoids discord import in middleware).
type CommandLogger interface {
	LogCommand(s *discordgo.Session, store *storage.Storage, guildID, channelID, userID, username, commandName string) error
}

// Discord-specific contexts (what the runtime passes when executing).
// Config is injected so handlers and middleware never call config.New().

type SlashInteractionContext struct {
	Session   *discordgo.Session
	Event     *discordgo.InteractionCreate
	Args      []string
	Storage   *storage.Storage
	Config    *config.Config
	Responder Responder
	Logger    CommandLogger
}

type ComponentInteractionContext struct {
	Session   *discordgo.Session
	Event     *discordgo.InteractionCreate
	Storage   *storage.Storage
	Config    *config.Config
	Responder Responder
	Logger    CommandLogger
}

type MessageReactionContext struct {
	Session *discordgo.Session
	Event   *discordgo.MessageReactionAdd
	Storage *storage.Storage
	Config  *config.Config
	Logger  CommandLogger
}

type MessageApplicationCommandContext struct {
	Session   *discordgo.Session
	Event     *discordgo.InteractionCreate
	Storage   *storage.Storage
	Target    *discordgo.Message
	Config    *config.Config
	Responder Responder
	Logger    CommandLogger
}

type MessageContext struct {
	Session *discordgo.Session
	Event   *discordgo.MessageCreate
	Storage *storage.Storage
	Config  *config.Config
}

// Providers — how a command is registered with Discord (slash, context menu, reaction).

type SlashProvider interface {
	SlashDefinition() *discordgo.ApplicationCommand
}

type ContextMenuProvider interface {
	ContextDefinition() *discordgo.ApplicationCommand
}

type ReactionProvider interface {
	ReactionDefinition() string
}

type ComponentInteractionHandler interface {
	Component(*ComponentInteractionContext) error
}

// DiscordMeta is exposed by the Discord adapter so middleware can read Group/Category/Permissions
// without depending on the concrete Discord command type.
type DiscordMeta interface {
	Group() string
	Category() string
	UserPermissions() []int64
}

// DiscordCommand is what individual Discord commands implement (Run takes interface{} for Discord contexts).
type DiscordCommand interface {
	Name() string
	Description() string
	Group() string
	Category() string
	UserPermissions() []int64
	Run(ctx interface{}) error
}

// DiscordAdapter adapts a DiscordCommand to commandkit.Command so it can live in the universal registry.
// It also implements SlashProvider, ContextMenuProvider, ReactionProvider, ComponentInteractionHandler,
// and DiscordMeta by delegating to the inner command.
type DiscordAdapter struct {
	Cmd DiscordCommand
}

func (a *DiscordAdapter) Name() string             { return a.Cmd.Name() }
func (a *DiscordAdapter) Description() string      { return a.Cmd.Description() }
func (a *DiscordAdapter) Group() string            { return a.Cmd.Group() }
func (a *DiscordAdapter) Category() string         { return a.Cmd.Category() }
func (a *DiscordAdapter) UserPermissions() []int64 { return a.Cmd.UserPermissions() }

func (a *DiscordAdapter) Run(ctx context.Context, inv *commandkit.Invocation) error {
	return a.Cmd.Run(inv.Data)
}

func (a *DiscordAdapter) SlashDefinition() *discordgo.ApplicationCommand {
	if sp, ok := a.Cmd.(SlashProvider); ok {
		return sp.SlashDefinition()
	}
	return nil
}

func (a *DiscordAdapter) ContextDefinition() *discordgo.ApplicationCommand {
	if cp, ok := a.Cmd.(ContextMenuProvider); ok {
		return cp.ContextDefinition()
	}
	return nil
}

func (a *DiscordAdapter) ReactionDefinition() string {
	if rp, ok := a.Cmd.(ReactionProvider); ok {
		return rp.ReactionDefinition()
	}
	return ""
}

func (a *DiscordAdapter) Component(ctx *ComponentInteractionContext) error {
	if ch, ok := a.Cmd.(ComponentInteractionHandler); ok {
		return ch.Component(ctx)
	}
	return nil
}

// ConfigFromInvocation returns the injected Config from inv.Data if it is a Discord context.
func ConfigFromInvocation(inv *commandkit.Invocation) *config.Config {
	if inv == nil || inv.Data == nil {
		return nil
	}
	switch v := inv.Data.(type) {
	case *SlashInteractionContext:
		return v.Config
	case *ComponentInteractionContext:
		return v.Config
	case *MessageReactionContext:
		return v.Config
	case *MessageApplicationCommandContext:
		return v.Config
	case *MessageContext:
		return v.Config
	default:
		return nil
	}
}

// RegisterCommand registers a Discord command with the universal registry and applies middlewares.
func RegisterCommand(discordCmd DiscordCommand, mws ...commandkit.Middleware) {
	c := commandkit.Apply(&DiscordAdapter{Cmd: discordCmd}, mws...)
	commandkit.DefaultRegistry.Register(c)
}
