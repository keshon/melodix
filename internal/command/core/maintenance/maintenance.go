package maintenance

import (
	"fmt"

	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/discord/perm"
)

type Maintenance struct{}

func (c *Maintenance) Name() string        { return "maintenance" }
func (c *Maintenance) Description() string { return "Bot maintenance commands" }
func (c *Maintenance) Group() string       { return "core" }
func (c *Maintenance) Category() string    { return "⚙️ Settings" }
func (c *Maintenance) UserPermissions() []int64 {
	return []int64{perm.Administrator}
}

func (c *Maintenance) SlashDefinition() *cmdadapter.SlashCommand {
	return &cmdadapter.SlashCommand{
		Name:        c.Name(),
		Description: c.Description(),
		Options: []cmdadapter.SlashOption{
			{
				Type:        cmdadapter.OptionSubCommand,
				Name:        "ping",
				Description: "Check bot latency",
			},
			{
				Type:        cmdadapter.OptionSubCommand,
				Name:        "export-data",
				Description: "Export the current server database as JSON",
			},
			{
				Type:        cmdadapter.OptionSubCommand,
				Name:        "status",
				Description: "Retrieve statistics about the guild",
			},
		},
	}
}

func (c *Maintenance) Run(ctx interface{}) error {
	context, ok := ctx.(*cmdadapter.SlashInteractionContext)
	if !ok {
		return nil
	}

	s := context.Session
	e := context.Event
	storage := context.Storage

	options := e.ApplicationCommandData().Options

	if len(options) == 0 {
		return context.RespondEphemeral(&cmdadapter.Embed{
			Description: "No subcommand provided.",
		})
	}

	sub := options[0]
	switch sub.Name {
	case "ping":
		return runPing(s, e)
	case "export-data":
		return runExportData(s, e, *storage)
	case "status":
		return runStatus(s, e, *storage)
	default:
		return context.RespondEphemeral(&cmdadapter.Embed{
			Description: fmt.Sprintf("Unknown subcommand: %s", sub.Name),
		})
	}
}
