package maintenance

import (
	"fmt"

	"github.com/keshon/melodix/internal/command"
	"github.com/keshon/melodix/internal/discord/respond"

	"github.com/bwmarrin/discordgo"
)

type Maintenance struct{}

func (c *Maintenance) Name() string        { return "maintenance" }
func (c *Maintenance) Description() string { return "Bot maintenance commands" }
func (c *Maintenance) Group() string       { return "core" }
func (c *Maintenance) Category() string    { return "⚙️ Settings" }
func (c *Maintenance) UserPermissions() []int64 {
	return []int64{discordgo.PermissionAdministrator}
}

func (c *Maintenance) SlashDefinition() *discordgo.ApplicationCommand {
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

func (c *Maintenance) Run(ctx interface{}) error {
	context, ok := ctx.(*command.SlashInteractionContext)
	if !ok {
		return nil
	}

	s := context.Session
	e := context.Event
	storage := context.Storage

	options := e.ApplicationCommandData().Options

	if len(options) == 0 {
		return respond.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Description: "No subcommand provided.",
		})
	}

	sub := options[0]
	switch sub.Name {
	case "ping":
		return runPing(s, e)
	case "download-db":
		return runDownloadDB(s, e, *storage)
	case "status":
		return runStatus(s, e, *storage)
	default:
		return respond.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Description: fmt.Sprintf("Unknown subcommand: %s", sub.Name),
		})
	}
}
