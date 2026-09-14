package commands

import (
	"fmt"

	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/storage"
)

// RunCmdEnable enables a command group for the guild.
func RunCmdEnable(ctx *cmdadapter.SlashInteractionContext, stor storage.Storage, syncer cmdadapter.CommandSyncer, sub cmdadapter.SlashArgument) error {
	group := subOptionString(sub, "group")
	return runCmdSetGroupState(ctx, stor, syncer, group, true)
}

// RunCmdDisable disables a command group for the guild.
func RunCmdDisable(ctx *cmdadapter.SlashInteractionContext, stor storage.Storage, syncer cmdadapter.CommandSyncer, sub cmdadapter.SlashArgument) error {
	group := subOptionString(sub, "group")
	return runCmdSetGroupState(ctx, stor, syncer, group, false)
}

func runCmdSetGroupState(ctx *cmdadapter.SlashInteractionContext, stor storage.Storage, syncer cmdadapter.CommandSyncer, group string, enabled bool) error {
	if group == "" {
		return ctx.RespondEphemeral(&cmdadapter.Embed{
			Description: "Missing required group option.",
		})
	}

	if group == "core" && !enabled {
		return ctx.RespondEphemeral(&cmdadapter.Embed{
			Description: "You can't disable the `core` group. It's the backbone of the discord.",
		})
	}

	var err error
	embed := &cmdadapter.Embed{
		Footer: "Use /settings commands status to check which commands are disabled.",
	}

	if enabled {
		err = stor.EnableGroup(ctx.Event.GuildID, group)
		if err != nil {
			embed.Description = "Failed to enable the group."
			return ctx.RespondEphemeral(embed)
		}
		embed.Description = fmt.Sprintf("Command/group `%s` enabled.", group)
	} else {
		err = stor.DisableGroup(ctx.Event.GuildID, group)
		if err != nil {
			embed.Description = "Failed to disable the group."
			return ctx.RespondEphemeral(embed)
		}
		embed.Description = fmt.Sprintf("Command/group `%s` disabled.", group)
	}

	if syncer != nil {
		_ = syncer.SyncGuildCommands(ctx.Event.GuildID)
	}

	return ctx.RespondEphemeral(embed)
}

func subOptionString(sub cmdadapter.SlashArgument, name string) string {
	for _, opt := range sub.Options {
		if opt.Name == name {
			return opt.StringValue()
		}
	}
	return ""
}
