package commands

import (
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/internal/discord/reply"
	"github.com/keshon/melodix/internal/storage"
)

// RunCmdLog shows recent command usage for the guild.
func RunCmdLog(s *discordgo.Session, e *discordgo.InteractionCreate, storage storage.Storage) error {
	guildID := e.GuildID

	records, err := storage.CommandHistory(guildID)
	if err != nil {
		return reply.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Description: "Failed to fetch command logs: " + err.Error(),
		})
	}
	if len(records) == 0 {
		return reply.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Description: "No command logs found.",
		})
	}

	var builder strings.Builder
	builder.WriteString("Datetime           \tUsername       \tChannel     \tCommand\n")

	for i := len(records) - 1; i >= 0; i-- {
		r := records[i]

		line := r.Datetime.Format("2006-01-02 15:04:05") + "\t" +
			r.Username + "\t#" + r.ChannelName + "\t/" + r.Command + "\n"

		if builder.Len()+len(line) > maxContentLength {
			break
		}
		builder.WriteString(line)
	}

	msg := codeLeftBlockWrapper + "\n" + builder.String() + codeRightBlockWrapper
	return reply.RespondEphemeral(s, e, msg)
}
