package maintenance

import (
	"fmt"

	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/discord/reply"
	"github.com/keshon/melodix/internal/storage"
)

func runStatus(ctx *cmdadapter.SlashInteractionContext, storage storage.Storage) error {
	guild, err := ctx.Session.State.Guild(ctx.Event.GuildID)
	if err != nil || guild == nil {
		guild, err = ctx.Session.Guild(ctx.Event.GuildID)
		if err != nil {
			return ctx.RespondEphemeral(&cmdadapter.Embed{
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

	return ctx.RespondEphemeral(&cmdadapter.Embed{
		Title:       "📊 Guild Status",
		Description: desc,
		Color:       reply.EmbedColor,
	})
}
