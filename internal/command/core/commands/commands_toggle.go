package commands

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/discord/reply"
	"github.com/keshon/melodix/internal/storage"
)

// RunCmdEnable enables a command group for the guild.
func RunCmdEnable(s *discordgo.Session, e *discordgo.InteractionCreate, stor storage.Storage, syncer cmdadapter.CommandSyncer, sub *discordgo.ApplicationCommandInteractionDataOption) error {
	group := subOptionString(sub, "group")
	return runCmdSetGroupState(s, e, stor, syncer, group, true)
}

// RunCmdDisable disables a command group for the guild.
func RunCmdDisable(s *discordgo.Session, e *discordgo.InteractionCreate, stor storage.Storage, syncer cmdadapter.CommandSyncer, sub *discordgo.ApplicationCommandInteractionDataOption) error {
	group := subOptionString(sub, "group")
	return runCmdSetGroupState(s, e, stor, syncer, group, false)
}

func runCmdSetGroupState(s *discordgo.Session, e *discordgo.InteractionCreate, stor storage.Storage, syncer cmdadapter.CommandSyncer, group string, enabled bool) error {
	if group == "" {
		return reply.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Description: "Missing required group option.",
		})
	}

	if group == "core" && !enabled {
		return reply.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Description: "You can't disable the `core` group. It's the backbone of the discord.",
		})
	}

	var err error
	embed := &discordgo.MessageEmbed{
		Footer: &discordgo.MessageEmbedFooter{Text: "Use /settings commands status to check which commands are disabled."},
	}

	if enabled {
		err = stor.EnableGroup(e.GuildID, group)
		if err != nil {
			embed.Description = "Failed to enable the group."
			return reply.RespondEmbedEphemeral(s, e, embed)
		}
		embed.Description = fmt.Sprintf("Command/group `%s` enabled.", group)
	} else {
		err = stor.DisableGroup(e.GuildID, group)
		if err != nil {
			embed.Description = "Failed to disable the group."
			return reply.RespondEmbedEphemeral(s, e, embed)
		}
		embed.Description = fmt.Sprintf("Command/group `%s` disabled.", group)
	}

	if syncer != nil {
		_ = syncer.SyncGuildCommands(e.GuildID)
	}

	return reply.RespondEmbedEphemeral(s, e, embed)
}

func subOptionString(sub *discordgo.ApplicationCommandInteractionDataOption, name string) string {
	for _, opt := range sub.Options {
		if opt.Name == name {
			return opt.StringValue()
		}
	}
	return ""
}
