package settings

import (
	"fmt"

	"github.com/keshon/melodix/internal/command/core/commands"
	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/discord/perm"

	"github.com/keshon/melodix/internal/storage"
)

type SettingsCommand struct{}

func (c *SettingsCommand) Name() string        { return "settings" }
func (c *SettingsCommand) Description() string { return "Server settings" }
func (c *SettingsCommand) Group() string       { return "core" }
func (c *SettingsCommand) Category() string    { return "⚙️ Settings" }
func (c *SettingsCommand) UserPermissions() []int64 {
	return []int64{perm.Administrator}
}

func (c *SettingsCommand) SlashDefinition() *cmdadapter.SlashCommand {
	return &cmdadapter.SlashCommand{
		Name:        c.Name(),
		Description: c.Description(),
		Options: []cmdadapter.SlashOption{
			{
				Type:        cmdadapter.OptionSubCommandGroup,
				Name:        "commands",
				Description: "Command group management",
				Options:     commands.CommandsSubcommandOptions(),
			},
		},
	}
}

func (c *SettingsCommand) Run(context *cmdadapter.SlashInteractionContext) error {

	st := context.Storage

	group, ok := context.FirstOption()
	if !ok {
		return context.RespondEphemeral(&cmdadapter.Embed{
			Description: "No settings group provided.",
		})
	}

	sub, ok := group.First()
	if !ok {
		return context.RespondEphemeral(&cmdadapter.Embed{
			Description: "No subcommand provided.",
		})
	}

	switch group.Name {
	case "commands":
		return runCommandsSettings(context, *st, context.Syncer, sub)
	default:
		return context.RespondEphemeral(&cmdadapter.Embed{
			Description: fmt.Sprintf("Unknown settings group: %s", group.Name),
		})
	}
}

func runCommandsSettings(ctx *cmdadapter.SlashInteractionContext, st storage.Storage, syncer cmdadapter.CommandSyncer, sub cmdadapter.SlashArgument) error {
	switch sub.Name {
	case "log":
		return commands.RunCmdLog(ctx, st)
	case "status":
		return commands.RunCmdStatus(ctx, st)
	case "enable":
		return commands.RunCmdEnable(ctx, st, syncer, sub)
	case "disable":
		return commands.RunCmdDisable(ctx, st, syncer, sub)
	default:
		return ctx.RespondEphemeral(&cmdadapter.Embed{
			Description: fmt.Sprintf("Unknown subcommand: %s", sub.Name),
		})
	}
}
