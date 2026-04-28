package help

import (
	"github.com/keshon/buildinfo"
	"github.com/keshon/melodix/internal/command"
	"github.com/keshon/melodix/internal/discord/respond"

	"github.com/bwmarrin/discordgo"
)

type Help struct{}

func (c *Help) Name() string        { return "help" }
func (c *Help) Description() string { return "Get a list of available commands" }
func (c *Help) Group() string       { return "core" }
func (c *Help) Category() string    { return "🕯️ Information" }
func (c *Help) UserPermissions() []int64 {
	return []int64{}
}

func (c *Help) SlashDefinition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        c.Name(),
		Description: c.Description(),
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "category",
				Description: "View commands grouped by category",
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "group",
				Description: "View commands grouped by group",
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "flat",
				Description: "View all commands as a flat list",
			},
		},
	}
}

func (c *Help) Run(ctx interface{}) error {
	context, ok := ctx.(*command.SlashInteractionContext)
	if !ok {
		return nil
	}

	session := context.Session
	event := context.Event

	if err := respond.RespondDeferredEphemeral(session, event); err != nil {
		context.AppLog.Error().Err(err).Msg("help_defer_failed")
		return err
	}

	data := event.ApplicationCommandData()
	if len(data.Options) == 0 {
		return respond.FollowupEmbedEphemeral(session, event, &discordgo.MessageEmbed{
			Description: "No subcommand provided. Use `category`, `group`, or `flat`.",
		})
	}

	var output string
	switch data.Options[0].Name {
	case "group":
		output = runHelpByGroup()
	case "flat":
		output = runHelpFlat()
	default:
		output = runHelpByCategory()
	}

	info := buildinfo.Get()
	embed := &discordgo.MessageEmbed{
		Title:       info.Project + " Help",
		Description: output,
		Color:       respond.EmbedColor,
	}

	return respond.FollowupEmbedEphemeral(session, event, embed)
}
