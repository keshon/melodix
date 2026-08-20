package queue

import (
	"fmt"

	"github.com/bwmarrin/discordgo"

	"github.com/keshon/melodix/internal/command/music/common"
	"github.com/keshon/melodix/internal/discord"
	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/discord/reply"
	"github.com/keshon/melodix/pkg/music/sources/youtube"
)

type Queue struct {
	Bot discord.VoiceAPI
}

func (c *Queue) Name() string             { return "queue" }
func (c *Queue) Description() string      { return "Show what is playing and what is queued next" }
func (c *Queue) Group() string            { return "music" }
func (c *Queue) Category() string         { return "🎵 Music" }
func (c *Queue) UserPermissions() []int64 { return []int64{} }

func (c *Queue) SlashDefinition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        c.Name(),
		Description: c.Description(),
	}
}

func (c *Queue) Run(ctx interface{}) error {
	slashCtx, ok := ctx.(*cmdadapter.SlashInteractionContext)
	if !ok {
		return nil
	}

	s := slashCtx.Session
	e := slashCtx.Event

	if err := s.InteractionRespond(e.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	}); err != nil {
		return fmt.Errorf("failed to send deferred response: %w", err)
	}

	p := c.Bot.GetOrCreatePlayer(e.GuildID)
	if p == nil {
		reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 Error",
			Description: "Music service is not available.",
		})
		return nil
	}

	// Read-only view: no voice state or permission check, and nothing is mutated.
	current := p.CurrentTrack()
	upcoming := p.Queue()

	embed := &discordgo.MessageEmbed{
		Title:       "🎵 Queue",
		Description: common.FormatQueueBody(current, upcoming),
		Color:       reply.EmbedColor,
	}
	if n := len(upcoming); n > 0 {
		noun := "tracks"
		if n == 1 {
			noun = "track"
		}
		// The per-link cap is named here because this is where someone counts the
		// tracks and wonders why a 300-track playlist became fewer. It is per
		// link, not per queue: several playlists still stack up.
		embed.Footer = &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("%d %s queued · up to %d per playlist link · skip with /next",
				n, noun, youtube.MaxPlaylistItems),
		}
	}
	reply.FollowupEmbedEphemeral(s, e, embed)
	return nil
}
