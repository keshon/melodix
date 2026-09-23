package commands

import (
	"fmt"

	"github.com/keshon/melodix/internal/discord/adapter"
	"github.com/keshon/melodix/internal/storage"
)

// RunCmdEnable enables a command group for the guild.
func RunCmdEnable(ctx *adapter.SlashInteractionContext, stor storage.Storage, syncer adapter.CommandSyncer, sub adapter.SlashArgument) error {
	group := subOptionString(sub, "group")
	return runCmdSetGroupState(ctx, stor, syncer, group, true)
}

// RunCmdDisable disables a command group for the guild.
func RunCmdDisable(ctx *adapter.SlashInteractionContext, stor storage.Storage, syncer adapter.CommandSyncer, sub adapter.SlashArgument) error {
	group := subOptionString(sub, "group")
	return runCmdSetGroupState(ctx, stor, syncer, group, false)
}

func runCmdSetGroupState(ctx *adapter.SlashInteractionContext, stor storage.Storage, syncer adapter.CommandSyncer, group string, enabled bool) error {
	if group == "" {
		return ctx.RespondEphemeral(&adapter.Embed{
			Description: "Missing required group option.",
		})
	}

	if group == "core" && !enabled {
		return ctx.RespondEphemeral(&adapter.Embed{
			Description: "You can't disable the `core` group. It's the backbone of the discord.",
		})
	}

	var err error
	embed := &adapter.Embed{
		Footer: "Use /settings commands status to check which commands are disabled.",
	}

	if enabled {
		err = stor.EnableGroup(ctx.GuildID(), group)
		if err != nil {
			embed.Description = "Failed to enable the group."
			return ctx.RespondEphemeral(embed)
		}
		embed.Description = fmt.Sprintf("Command/group `%s` enabled.", group)
	} else {
		err = stor.DisableGroup(ctx.GuildID(), group)
		if err != nil {
			embed.Description = "Failed to disable the group."
			return ctx.RespondEphemeral(embed)
		}
		embed.Description = fmt.Sprintf("Command/group `%s` disabled.", group)
	}

	if syncer != nil {
		_ = syncer.SyncGuildCommands(ctx.GuildID())
	}

	return ctx.RespondEphemeral(embed)
}

func subOptionString(sub adapter.SlashArgument, name string) string {
	for _, opt := range sub.Options {
		if opt.Name == name {
			return opt.StringValue()
		}
	}
	return ""
}
