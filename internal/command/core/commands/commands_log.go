package commands

import (
	"strings"

	"github.com/keshon/melodix/internal/discord/adapter"
	"github.com/keshon/melodix/internal/storage"
)

// RunCmdLog shows recent command usage for the guild.
func RunCmdLog(ctx *adapter.SlashInteractionContext, storage storage.Storage) error {
	guildID := ctx.GuildID()

	records, err := storage.CommandHistory(guildID)
	if err != nil {
		return ctx.RespondEphemeral(&adapter.Embed{
			Description: "Failed to fetch command logs: " + err.Error(),
		})
	}
	if len(records) == 0 {
		return ctx.RespondEphemeral(&adapter.Embed{
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
	return ctx.RespondWith(adapter.Reply{Text: msg, Ephemeral: true})
}
