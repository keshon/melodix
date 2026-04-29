package stop

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/internal/command"
	"github.com/keshon/melodix/internal/discord"
	"github.com/keshon/melodix/internal/discord/discordreply"
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
	slashCtx, ok := ctx.(*command.SlashInteractionContext)
	if !ok {
		return nil
	}

	s := slashCtx.Session
	e := slashCtx.Event

	if err := s.InteractionRespond(e.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	}); err != nil {
		return fmt.Errorf("failed to defer response: %w", err)
	}

	player := c.Bot.GetOrCreatePlayer(e.GuildID)
	if err := player.Stop(true); err != nil {
		slashCtx.AppLog.Warn().Err(err).Msg("player_stop_failed")
	}
	stopMsg := "Playback stopped. Queue cleared."
	if err := discordreply.FollowupEmbed(s, e, &discordgo.MessageEmbed{
		Description: "⏹️ " + stopMsg,
	}); err != nil {
		slashCtx.AppLog.Warn().Str("command", "stop").Err(err).Msg("followup_embed_failed")
		_ = discordreply.EditResponse(s, e, "⏹️ "+stopMsg)
	}
	return nil
}

