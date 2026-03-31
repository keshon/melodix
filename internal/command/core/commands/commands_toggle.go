package commands

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/internal/discord"
	"github.com/keshon/melodix/internal/storage"
)

func (c *Commands) runCmdToggle(s *discordgo.Session, e *discordgo.InteractionCreate, storage storage.Storage) error {
	data := e.ApplicationCommandData()

	subOptions := data.Options[0].Options

	var group, state string
	for _, opt := range subOptions {
		switch opt.Name {
		case "group":
			group = opt.StringValue()
		case "state":
			state = opt.StringValue()
		}
	}

	if group == "core" && state == "disable" {
		return discord.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Description: "You can't disable the `core` group. It's the backbone of the discord.",
		})
	}

	var err error
	embed := &discordgo.MessageEmbed{
		Footer: &discordgo.MessageEmbedFooter{Text: "Use /commands status to check which commands are disabled."},
	}

	if state == "disable" {
		err = storage.DisableGroup(e.GuildID, group)
		if err != nil {
			embed.Description = "Failed to disable the group."
			return discord.RespondEmbedEphemeral(s, e, embed)
		}
		embed.Description = fmt.Sprintf("Command/group `%s` disabled.", group)
	} else {
		err = storage.EnableGroup(e.GuildID, group)
		if err != nil {
			embed.Description = "Failed to enable the group."
			return discord.RespondEmbedEphemeral(s, e, embed)
		}
		embed.Description = fmt.Sprintf("Command/group `%s` enabled.", group)
	}

	// Publish event to refresh commands
	discord.PublishSystemEvent(discord.SystemEvent{
		Type:    discord.SystemEventRefreshCommands,
		GuildID: e.GuildID,
		Target:  "group:" + group,
	})

	return discord.RespondEmbedEphemeral(s, e, embed)
}
