package resume

import (
	"fmt"

	"github.com/keshon/melodix/internal/discord"
	"github.com/keshon/melodix/internal/discord/adapter"
	"github.com/keshon/melodix/internal/discord/reply"
)

type Command struct {
	Bot discord.VoiceAPI
}

func (c *Command) Name() string             { return "resume" }
func (c *Command) Description() string      { return "Resume the current track" }
func (c *Command) Group() string            { return "music" }
func (c *Command) Category() string         { return "🎵 Music" }
func (c *Command) UserPermissions() []int64 { return nil }

func (c *Command) SlashDefinition() *adapter.SlashCommand {
	return &adapter.SlashCommand{Name: c.Name(), Description: c.Description()}
}

func (c *Command) Run(ctx *adapter.SlashInteractionContext) error {
	err := ctx.Defer()
	if err != nil {
		return fmt.Errorf("deferring response: %w", err)
	}

	svc := c.Bot.Music()
	if svc == nil {
		return ctx.FollowupEphemeral(&adapter.Embed{Description: "Music service is not available.", Color: reply.EmbedColor})
	}
	p := svc.Player(ctx.GuildID())
	if !p.IsPlaying() {
		return ctx.FollowupEphemeral(&adapter.Embed{Description: "No track is currently playing.", Color: reply.EmbedColor})
	}
	state, err := c.Bot.FindUserVoiceState(ctx.GuildID(), ctx.UserID())
	if err != nil || state.ChannelID != p.Target() {
		return ctx.FollowupEphemeral(&adapter.Embed{Description: "Join the bot's voice channel first.", Color: reply.EmbedColor})
	}

	err = p.Resume()
	if err != nil {
		return ctx.FollowupEphemeral(&adapter.Embed{Description: fmt.Sprintf("Cannot resume: %v.", err), Color: reply.EmbedColor})
	}
	return ctx.Followup(&adapter.Embed{Description: "▶️ Playback resumed.", Color: reply.EmbedColor})
}
