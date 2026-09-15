package maintenance

import (
	"fmt"

	"github.com/keshon/melodix/internal/discord/adapter"
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

func (c *Maintenance) SlashDefinition() *adapter.SlashCommand {
	return &adapter.SlashCommand{
		Name:        c.Name(),
		Description: c.Description(),
		Options: []adapter.SlashOption{
			{
				Type:        adapter.OptionSubCommand,
				Name:        "ping",
				Description: "Check bot latency",
			},
			{
				Type:        adapter.OptionSubCommand,
				Name:        "export-data",
				Description: "Export the current server database as JSON",
			},
			{
				Type:        adapter.OptionSubCommand,
				Name:        "status",
				Description: "Retrieve statistics about the guild",
			},
		},
	}
}

func (c *Maintenance) Run(context *adapter.SlashInteractionContext) error {

	storage := context.Storage

	sub, ok := context.FirstOption()
	if !ok {
		return context.RespondEphemeral(&adapter.Embed{
			Description: "No subcommand provided.",
		})
	}

	switch sub.Name {
	case "ping":
		return runPing(context)
	case "export-data":
		return runExportData(context, *storage)
	case "status":
		return runStatus(context, *storage)
	default:
		return context.RespondEphemeral(&adapter.Embed{
			Description: fmt.Sprintf("Unknown subcommand: %s", sub.Name),
		})
	}
}
