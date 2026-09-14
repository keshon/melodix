package maintenance

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/discord/reply"
	"github.com/keshon/melodix/internal/storage"
)

func runExportData(ctx *cmdadapter.SlashInteractionContext, storage storage.Storage) error {
	guildID := ctx.Event.GuildID
	record, err := storage.ExportGuild(guildID)
	if err != nil {
		return ctx.RespondEphemeral(&cmdadapter.Embed{
			Description: fmt.Sprintf("Failed to fetch record: ```%v```", err),
			Color:       reply.EmbedColor,
		})
	}

	jsonBytes, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return ctx.RespondEphemeral(&cmdadapter.Embed{
			Description: fmt.Sprintf("JSON encode failed: ```%v```", err),
			Color:       reply.EmbedColor,
		})
	}

	embed := &cmdadapter.Embed{
		Title:       "🧠 Database Dump",
		Description: "Here’s your current in-memory datastore snapshot.",
		Color:       reply.EmbedColor,
	}

	fileName := fmt.Sprintf("%s_database_dump.json", guildID)
	return reply.RespondEmbedEphemeralWithFile(ctx.Session, ctx.Event, embed, bytes.NewReader(jsonBytes), fileName)
}
