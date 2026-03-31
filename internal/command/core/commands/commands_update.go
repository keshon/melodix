package commands

import (
	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/internal/discord"
)

func (c *Commands) runCmdUpdate(s *discordgo.Session, e *discordgo.InteractionCreate) error {
	subOptions := e.ApplicationCommandData().Options[0].Options

	var target string
	if len(subOptions) > 0 {
		target = subOptions[0].StringValue()
	}

	discord.PublishSystemEvent(discord.SystemEvent{
		Type:    discord.SystemEventRefreshCommands,
		GuildID: e.GuildID,
		Target:  target,
	})

	return discord.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
		Description: "Command update requested — it may take some time to apply.",
	})
}
