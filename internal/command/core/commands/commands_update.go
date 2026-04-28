package commands

import (
	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/internal/discord/respond"
	"github.com/keshon/melodix/internal/discord/systemevents"
)

func (c *Commands) runCmdUpdate(s *discordgo.Session, e *discordgo.InteractionCreate, bus *systemevents.Bus) error {
	subOptions := e.ApplicationCommandData().Options[0].Options

	var target string
	if len(subOptions) > 0 {
		target = subOptions[0].StringValue()
	}

	if bus != nil {
		bus.Emit(systemevents.Event{
			Type:    systemevents.EventRefreshCommands,
			GuildID: e.GuildID,
			Target:  target,
		})
	}

	return respond.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
		Description: "Command update requested — it may take some time to apply.",
	})
}

func emitRefreshCommands(bus *systemevents.Bus, guildID, target string) {
	if bus == nil {
		return
	}
	bus.Emit(systemevents.Event{
		Type:    systemevents.EventRefreshCommands,
		GuildID: guildID,
		Target:  target,
	})
}
