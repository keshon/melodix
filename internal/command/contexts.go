package command

import (
	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/discord/systemevents"
	"github.com/keshon/melodix/internal/storage"
	"github.com/rs/zerolog"
)

type SlashInteractionContext struct {
	Session   *discordgo.Session
	Event     *discordgo.InteractionCreate
	Args      []string
	Storage   *storage.Storage
	Config    *config.Config
	Responder Responder
	Logger    Logger
	AppLog    zerolog.Logger
	SystemBus *systemevents.Bus
}

type ComponentInteractionContext struct {
	Session   *discordgo.Session
	Event     *discordgo.InteractionCreate
	Storage   *storage.Storage
	Config    *config.Config
	Responder Responder
	Logger    Logger
	AppLog    zerolog.Logger
}

type MessageReactionContext struct {
	Session *discordgo.Session
	Event   *discordgo.MessageReactionAdd
	Storage *storage.Storage
	Config  *config.Config
	Logger  Logger
}

type MessageApplicationCommandContext struct {
	Session   *discordgo.Session
	Event     *discordgo.InteractionCreate
	Storage   *storage.Storage
	Target    *discordgo.Message
	Config    *config.Config
	Responder Responder
	Logger    Logger
	AppLog    zerolog.Logger
}

type MessageContext struct {
	Session *discordgo.Session
	Event   *discordgo.MessageCreate
	Storage *storage.Storage
	Config  *config.Config
}
