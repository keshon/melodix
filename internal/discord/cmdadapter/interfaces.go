package cmdadapter

import "github.com/bwmarrin/discordgo"

// Responder abstracts interaction replies so commands never import the discord
// package directly (avoids import cycles); reply.DefaultResponder implements it.
type Responder interface {
	RespondEmbedEphemeral(s *discordgo.Session, e *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) error
	RespondEmbed(s *discordgo.Session, e *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) error
	CheckBotPermissions(s *discordgo.Session, channelID string) bool
	EmbedColor() int
}

// Logger persists command invocations (implemented by cmdlogger).
type Logger interface {
	LogCommand(guildID, channelID, userID, username, commandName string) error
}

// SlashProvider is implemented by commands that expose a slash definition.
type SlashProvider interface {
	SlashDefinition() *discordgo.ApplicationCommand
}

// ContextMenuProvider is implemented by commands that expose a context-menu definition.
type ContextMenuProvider interface {
	ContextDefinition() *discordgo.ApplicationCommand
}

// ReactionProvider is implemented by commands triggered by a message reaction.
type ReactionProvider interface {
	ReactionDefinition() string
}

// ComponentInteractionHandler is implemented by commands that handle message
// components (buttons/selects) whose customID matches the command name.
type ComponentInteractionHandler interface {
	Component(*ComponentInteractionContext) error
}

// Meta is the read-side view of a command's classification, used by consumers
// that only group/filter commands (readme generation, middleware checks).
type Meta interface {
	Group() string
	Category() string
	UserPermissions() []int64
}

// Handler is the interface every melodix command implements; Meta is embedded
// so classification is defined in exactly one place.
type Handler interface {
	Meta
	Name() string
	Description() string
	Run(ctx interface{}) error
}
