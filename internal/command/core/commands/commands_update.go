package commands

import (
	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/internal/command"
	"github.com/keshon/melodix/internal/discord/discordreply"
)

func (c *Commands) runCmdUpdate(s *discordgo.Session, e *discordgo.InteractionCreate, syncer command.CommandSyncer) error {
	if syncer != nil {
		_ = syncer.SyncGuildCommands(e.GuildID)
	}
	return discordreply.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
		Description: "Command update requested — it may take some time to apply.",
	})
}
