package settings

import (
	"fmt"

	"github.com/keshon/melodix/internal/discord/adapter"
	"github.com/keshon/melodix/internal/discord/command/core/commands"
	"github.com/keshon/melodix/internal/discord/perm"

	"github.com/keshon/melodix/internal/storage"
)

type Command struct{}

func (c *Command) Name() string        { return "settings" }
func (c *Command) Description() string { return "Server settings" }
func (c *Command) Group() string       { return "core" }
func (c *Command) Category() string    { return "⚙️ Settings" }
func (c *Command) UserPermissions() []int64 {
	return []int64{perm.Administrator}
}

func (c *Command) SlashDefinition() *adapter.SlashCommand {
	return &adapter.SlashCommand{
		Name:        c.Name(),
		Description: c.Description(),
		Options: []adapter.SlashOption{
			{
				Type:        adapter.OptionSubCommandGroup,
				Name:        "commands",
				Description: "Command group management",
				Options:     commands.CommandsSubcommandOptions(),
			},
		},
	}
}

func (c *Command) Run(context *adapter.SlashInteractionContext) error {

	st := context.Storage

	group, ok := context.FirstOption()
	if !ok {
		return context.RespondEphemeral(&adapter.Embed{
			Description: "No settings group provided.",
		})
	}

	sub, ok := group.First()
	if !ok {
		return context.RespondEphemeral(&adapter.Embed{
			Description: "No subcommand provided.",
		})
	}

	switch group.Name {
	case "commands":
		return runCommandsSettings(context, *st, context.Syncer, sub)
	default:
		return context.RespondEphemeral(&adapter.Embed{
			Description: fmt.Sprintf("Unknown settings group: %s", group.Name),
		})
	}
}

func runCommandsSettings(ctx *adapter.SlashInteractionContext, st storage.Storage, syncer adapter.CommandSyncer, sub adapter.SlashArgument) error {
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
		return ctx.RespondEphemeral(&adapter.Embed{
			Description: fmt.Sprintf("Unknown subcommand: %s", sub.Name),
		})
	}
}
