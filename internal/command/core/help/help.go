package help

import (
	"github.com/keshon/buildinfo"
	"github.com/keshon/melodix/internal/discord/adapter"
	"github.com/keshon/melodix/internal/discord/reply"
)

type Help struct{}

func (c *Help) Name() string        { return "help" }
func (c *Help) Description() string { return "Get a list of available commands" }
func (c *Help) Group() string       { return "core" }
func (c *Help) Category() string    { return "ℹ️ Information" }
func (c *Help) UserPermissions() []int64 {
	return []int64{}
}

func (c *Help) SlashDefinition() *adapter.SlashCommand {
	return &adapter.SlashCommand{
		Name:        c.Name(),
		Description: c.Description(),
		Options: []adapter.SlashOption{
			{
				Type:        adapter.OptionSubCommand,
				Name:        "category",
				Description: "View commands grouped by category",
			},
			{
				Type:        adapter.OptionSubCommand,
				Name:        "group",
				Description: "View commands grouped by group",
			},
			{
				Type:        adapter.OptionSubCommand,
				Name:        "flat",
				Description: "View all commands as a flat list",
			},
		},
	}
}

func (c *Help) Run(context *adapter.SlashInteractionContext) error {

	if err := context.DeferEphemeral(); err != nil {
		context.AppLog.Error().Err(err).Msg("help_defer_failed")
		return err
	}

	sub, ok := context.FirstOption()
	if !ok {
		return context.FollowupEphemeral(&adapter.Embed{
			Description: "No subcommand provided. Use `category`, `group`, or `flat`.",
		})
	}

	var output string
	switch sub.Name {
	case "group":
		output = runHelpByGroup()
	case "flat":
		output = runHelpFlat()
	default:
		output = runHelpByCategory()
	}

	info := buildinfo.Get()
	embed := &adapter.Embed{
		Title:       info.Project + " Help",
		Description: output,
		Color:       reply.EmbedColor,
	}

	return context.FollowupEphemeral(embed)
}
