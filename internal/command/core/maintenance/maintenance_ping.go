package maintenance

import (
	"fmt"

	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/discord/reply"
)

func runPing(ctx *cmdadapter.SlashInteractionContext) error {

	latency := ctx.Latency().Milliseconds()
	return ctx.RespondEphemeral(&cmdadapter.Embed{
		Title:       "Pong! 🏓",
		Description: fmt.Sprintf("Latency: %dms", latency),
		Color:       reply.EmbedColor,
	})
}
