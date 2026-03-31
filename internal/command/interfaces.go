package command

import "github.com/bwmarrin/discordgo"

type Responder interface {
	RespondEmbedEphemeral(s *discordgo.Session, e *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) error
	RespondEmbed(s *discordgo.Session, e *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) error
	CheckBotPermissions(s *discordgo.Session, channelID string) bool
	EmbedColor() int
}

type Logger interface {
	LogCommand(guildID, channelID, userID, username, commandName string) error
}

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

type Meta interface {
	Group() string
	Category() string
	UserPermissions() []int64
}

type Handler interface {
	Name() string
	Description() string
	Group() string
	Category() string
	UserPermissions() []int64
	Run(ctx interface{}) error
}
