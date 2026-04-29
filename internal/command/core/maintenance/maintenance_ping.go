package maintenance

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/internal/discord/discordreply"
)

func runPing(s *discordgo.Session, e *discordgo.InteractionCreate) error {

	latency := s.HeartbeatLatency().Milliseconds()
	return discordreply.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
		Title:       "Pong! 🏓",
		Description: fmt.Sprintf("Latency: %dms", latency),
		Color:       discordreply.EmbedColor,
	})
}
