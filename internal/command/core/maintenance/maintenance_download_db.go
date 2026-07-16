package maintenance

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/internal/discord/reply"
	"github.com/keshon/melodix/internal/storage"
)

func runDownloadDB(s *discordgo.Session, e *discordgo.InteractionCreate, storage storage.Storage) error {
	guildID := e.GuildID
	record, err := storage.GuildRecord(guildID)
	if err != nil {
		return reply.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Description: fmt.Sprintf("Failed to fetch record: ```%v```", err),
			Color:       reply.EmbedColor,
		})
	}

	jsonBytes, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return reply.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Description: fmt.Sprintf("JSON encode failed: ```%v```", err),
			Color:       reply.EmbedColor,
		})
	}

	embed := &discordgo.MessageEmbed{
		Title:       "🧠 Database Dump",
		Description: "Here’s your current in-memory datastore snapshot.",
		Color:       reply.EmbedColor,
	}

	fileName := fmt.Sprintf("%s_database_dump.json", guildID)
	return reply.RespondEmbedEphemeralWithFile(s, e, embed, bytes.NewReader(jsonBytes), fileName)
}
