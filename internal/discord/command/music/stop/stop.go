package stop

import (
	"fmt"

	"github.com/keshon/melodix/internal/discord"
	"github.com/keshon/melodix/internal/discord/adapter"
)

type Command struct {
	Bot discord.VoiceAPI
}

func (c *Command) Name() string             { return "stop" }
func (c *Command) Description() string      { return "Stop playback and clear queue" }
func (c *Command) Group() string            { return "music" }
func (c *Command) Category() string         { return "🎵 Music" }
func (c *Command) UserPermissions() []int64 { return []int64{} }

func (c *Command) SlashDefinition() *adapter.SlashCommand {
	return &adapter.SlashCommand{
		Name:        c.Name(),
		Description: c.Description(),
	}
}

func (c *Command) Run(slashCtx *adapter.SlashInteractionContext) error {

	if err := slashCtx.Defer(); err != nil {
		return fmt.Errorf("failed to defer response: %w", err)
	}

	player := c.Bot.GetOrCreatePlayer(slashCtx.GuildID())
	if player == nil {
		_ = slashCtx.FollowupEphemeral(&adapter.Embed{
			Title:       "🎵 Error",
			Description: "Music service is not available.",
		})
		return nil
	}
	if err := player.Stop(true); err != nil {
		slashCtx.AppLog.Warn().Err(err).Msg("player_stop_failed")
	}
	stopMsg := "Playback stopped. Queue cleared."
	if err := slashCtx.Followup(&adapter.Embed{
		Description: "⏹️ " + stopMsg,
	}); err != nil {
		slashCtx.AppLog.Warn().Str("command", "stop").Err(err).Msg("followup_embed_failed")
		_ = slashCtx.EditResponseText("⏹️ " + stopMsg)
	}
	return nil
}
