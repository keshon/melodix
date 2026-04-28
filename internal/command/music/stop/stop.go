package stop

import (
	"fmt"
	"log"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/internal/command"
	"github.com/keshon/melodix/internal/discord"
	"github.com/keshon/melodix/internal/discord/respond"
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
		log.Printf("[WARN] Stop error: %v", err)
	}
	stopMsg := "Playback stopped. Queue cleared."
	if err := respond.FollowupEmbed(s, e, &discordgo.MessageEmbed{
		Description: "⏹️ " + stopMsg,
	}); err != nil {
		log.Printf("[WARN] FollowupEmbed failed for /stop: %v", err)
		_ = respond.EditResponse(s, e, "⏹️ "+stopMsg)
	}
	return nil
}

