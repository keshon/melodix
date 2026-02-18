package core

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/keshon/melodix/internal/command"
	"github.com/keshon/melodix/internal/discord"
	"github.com/keshon/melodix/internal/middleware"
	"github.com/keshon/melodix/internal/storage"

	"github.com/bwmarrin/discordgo"
)

type MaintenanceCommand struct{}

func (c *MaintenanceCommand) Name() string        { return "maintenance" }
func (c *MaintenanceCommand) Description() string { return "Bot maintenance commands" }
func (c *MaintenanceCommand) Group() string       { return "core" }
func (c *MaintenanceCommand) Category() string    { return "⚙️ Settings" }
func (c *MaintenanceCommand) UserPermissions() []int64 {
	return []int64{discordgo.PermissionAdministrator}
}

func (c *MaintenanceCommand) SlashDefinition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        c.Name(),
		Description: c.Description(),
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "ping",
				Description: "Check bot latency",
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "download-db",
				Description: "Download the current server database as a JSON file",
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "status",
				Description: "Retrieve statistics about the guild",
			},
		},
	}
}

func (c *MaintenanceCommand) Run(ctx interface{}) error {
	context, ok := ctx.(*command.SlashInteractionContext)
	if !ok {
		return nil
	}

	s := context.Session
	e := context.Event
	storage := context.Storage

	options := e.ApplicationCommandData().Options

	if len(options) == 0 {
		return discord.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Description: "No subcommand provided.",
		})
	}

	sub := options[0]
	switch sub.Name {
	case "ping":
		latency := s.HeartbeatLatency().Milliseconds()
		return discord.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "Pong! 🏓",
			Description: fmt.Sprintf("Latency: %dms", latency),
			Color:       discord.EmbedColor,
		})
	case "download-db":
		return runGetDB(s, e, *storage)
	case "status":
		return runStatus(s, e, *storage)
	default:
		return discord.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Description: fmt.Sprintf("Unknown subcommand: %s", sub.Name),
		})
	}
}

func runGetDB(s *discordgo.Session, e *discordgo.InteractionCreate, storage storage.Storage) error {
	guildID := e.GuildID
	record, err := storage.GetGuildRecord(guildID)
	if err != nil {
		return discord.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Description: fmt.Sprintf("Failed to fetch record: ```%v```", err),
			Color:       discord.EmbedColor,
		})
	}

	jsonBytes, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return discord.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Description: fmt.Sprintf("JSON encode failed: ```%v```", err),
			Color:       discord.EmbedColor,
		})
	}

	embed := &discordgo.MessageEmbed{
		Title:       "🧠 Database Dump",
		Description: "Here’s your current in-memory datastore snapshot.",
		Color:       discord.EmbedColor,
	}

	fileName := fmt.Sprintf("%s_database_dump.json", guildID)
	return discord.RespondEmbedEphemeralWithFile(s, e, embed, bytes.NewReader(jsonBytes), fileName)
}

func runStatus(s *discordgo.Session, e *discordgo.InteractionCreate, storage storage.Storage) error {
	guild, err := s.State.Guild(e.GuildID)
	if err != nil || guild == nil {
		guild, err = s.Guild(e.GuildID)
		if err != nil {
			return discord.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
				Description: fmt.Sprintf("Failed to fetch guild: %v", err),
				Color:       discord.EmbedColor,
			})
		}
	}

	// Gather statistics
	memberCount := len(guild.Members)
	roleCount := len(guild.Roles)
	channelCount := len(guild.Channels)

	// Build message
	desc := fmt.Sprintf(
		"**Guild name: %s**\n"+
			"**Guild ID: %s**\n"+
			"**Guild statistics:**\n"+
			"- Members: %d\n"+
			"- Roles: %d\n"+
			"- Channels: %d\n",
		guild.Name,
		guild.ID,
		memberCount,
		roleCount,
		channelCount,
	)

	return discord.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
		Title:       "📊 Guild Status",
		Description: desc,
		Color:       discord.EmbedColor,
	})
}

func init() {
	command.RegisterCommand(
		&MaintenanceCommand{},
		middleware.WithGroupAccessCheck(),
		middleware.WithGuildOnly(),
		middleware.WithUserPermissionCheck(),
		middleware.WithCommandLogger(),
	)
}
