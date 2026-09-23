package maintenance

import (
	"fmt"

	"github.com/keshon/melodix/internal/discord/adapter"
	"github.com/keshon/melodix/internal/discord/reply"
)

func runPing(ctx *adapter.SlashInteractionContext) error {

	latency := ctx.Latency().Milliseconds()
	return ctx.RespondEphemeral(&adapter.Embed{
		Title:       "Pong! 🏓",
		Description: fmt.Sprintf("Latency: %dms", latency),
		Color:       reply.EmbedColor,
	})
}
