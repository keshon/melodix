package maintenance

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/discord/reply"
	"github.com/keshon/melodix/internal/storage"
)

func runStatus(s *discordgo.Session, e *discordgo.InteractionCreate, storage storage.Storage) error {
	guild, err := s.State.Guild(e.GuildID)
	if err != nil || guild == nil {
		guild, err = s.Guild(e.GuildID)
		if err != nil {
			return reply.RespondEmbedEphemeral(s, e, &cmdadapter.Embed{
				Description: fmt.Sprintf("Failed to fetch guild: %v", err),
				Color:       reply.EmbedColor,
			})
		}
	}

	memberCount := len(guild.Members)
	roleCount := len(guild.Roles)
	channelCount := len(guild.Channels)

	desc := fmt.Sprintf(
		"**Guild name: %s**\n"+
			"**Guild ID: %s**\n"+
			"**Guild statistics:**\n"+
			"- Members: %d\n"+
			"- Roles: %d\n"+
			"- Channels: %d\n",
		guild.Name,
		guild.ID,
		memberCount,
		roleCount,
		channelCount,
	)

	return reply.RespondEmbedEphemeral(s, e, &cmdadapter.Embed{
		Title:       "📊 Guild Status",
		Description: desc,
		Color:       reply.EmbedColor,
	})
}
