package commands

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/internal/discord/respond"
	"github.com/keshon/melodix/internal/storage"
)

func (c *Commands) runCmdLog(s *discordgo.Session, e *discordgo.InteractionCreate, storage storage.Storage) error {
	guildID := e.GuildID

	records, err := storage.CommandHistory(guildID)
	if err != nil {
		return respond.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Description: fmt.Sprintf("Failed to fetch command logs: %v", err),
		})
	}
	if len(records) == 0 {
		return respond.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Description: "No command logs found.",
		})
	}

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("%-19s\t%-15s\t%-12s\t%s\n", "# Datetime", "# Username", "# Channel", "# Command"))

	for i := len(records) - 1; i >= 0; i-- {
		r := records[i]

		username := r.Username

		line := fmt.Sprintf("%-19s\t%-15s\t#%-12s\t/%s\n",
			r.Datetime.Format("2006-01-02 15:04:05"),
			username,
			r.ChannelName,
			r.Command,
		)

		if builder.Len()+len(line) > maxContentLength {
			break
		}
		builder.WriteString(line)
	}

	msg := codeLeftBlockWrapper + "\n" + builder.String() + codeRightBlockWrapper
	return respond.RespondEphemeral(s, e, msg)
}
