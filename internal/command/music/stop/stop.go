package stop

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/internal/discord"
	"github.com/keshon/melodix/internal/discord/cmdadapter"
)

type Stop struct {
	Bot discord.VoiceAPI
}

func (c *Stop) Name() string             { return "stop" }
func (c *Stop) Description() string      { return "Stop playback and clear queue" }
func (c *Stop) Group() string            { return "music" }
func (c *Stop) Category() string         { return "🎵 Music" }
func (c *Stop) UserPermissions() []int64 { return []int64{} }

func (c *Stop) SlashDefinition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        c.Name(),
		Description: c.Description(),
	}
}

func (c *Stop) Run(ctx interface{}) error {
	slashCtx, ok := ctx.(*cmdadapter.SlashInteractionContext)
	if !ok {
		return nil
	}

	if err := slashCtx.Defer(); err != nil {
		return fmt.Errorf("failed to defer response: %w", err)
	}

	player := c.Bot.GetOrCreatePlayer(slashCtx.GuildID())
	if player == nil {
		_ = slashCtx.FollowupEphemeral(&cmdadapter.Embed{
			Title:       "🎵 Error",
			Description: "Music service is not available.",
		})
		return nil
	}
	if err := player.Stop(true); err != nil {
		slashCtx.AppLog.Warn().Err(err).Msg("player_stop_failed")
	}
	stopMsg := "Playback stopped. Queue cleared."
	if err := slashCtx.Followup(&cmdadapter.Embed{
		Description: "⏹️ " + stopMsg,
	}); err != nil {
		slashCtx.AppLog.Warn().Str("command", "stop").Err(err).Msg("followup_embed_failed")
		_ = slashCtx.EditResponseText("⏹️ " + stopMsg)
	}
	return nil
}
