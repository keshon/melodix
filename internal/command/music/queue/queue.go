package queue

import (
	"fmt"

	"github.com/keshon/melodix/internal/command/music/common"
	"github.com/keshon/melodix/internal/discord"
	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/discord/reply"
	"github.com/keshon/melodix/pkg/music/parsers"
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

func (c *Queue) SlashDefinition() *cmdadapter.SlashCommand {
	return &cmdadapter.SlashCommand{
		Name:        c.Name(),
		Description: c.Description(),
	}
}

func (c *Queue) Run(ctx interface{}) error {
	slashCtx, ok := ctx.(*cmdadapter.SlashInteractionContext)
	if !ok {
		return nil
	}

	if err := slashCtx.Defer(); err != nil {
		return fmt.Errorf("failed to send deferred response: %w", err)
	}

	p := c.Bot.GetOrCreatePlayer(slashCtx.GuildID())
	if p == nil {
		slashCtx.FollowupEphemeral(&cmdadapter.Embed{
			Title:       "🎵 Error",
			Description: "Music service is not available.",
		})
		return nil
	}

	// Read-only view: no voice state or permission check, and nothing is mutated.
	current, playing := p.CurrentTrack()
	upcoming := p.Queue()
	// FormatQueueBody reads nil as "nothing playing". The pointer is to this
	// function's own copy, which nothing else writes.
	var nowPlaying *parsers.Track
	if playing {
		nowPlaying = &current
	}

	embed := &cmdadapter.Embed{
		Title:       "🎵 Queue",
		Description: common.FormatQueueBody(nowPlaying, upcoming),
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
		embed.Footer = fmt.Sprintf("%d %s queued · up to %d per playlist link · skip with /next",
			n, noun, youtube.MaxPlaylistItems)
	}
	slashCtx.FollowupEphemeral(embed)
	return nil
}
