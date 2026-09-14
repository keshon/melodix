package maintenance

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/discord/reply"
)

func runPing(s *discordgo.Session, e *discordgo.InteractionCreate) error {

	latency := s.HeartbeatLatency().Milliseconds()
	return reply.RespondEmbedEphemeral(s, e, &cmdadapter.Embed{
		Title:       "Pong! 🏓",
		Description: fmt.Sprintf("Latency: %dms", latency),
		Color:       reply.EmbedColor,
	})
}
