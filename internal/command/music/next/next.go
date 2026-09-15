package next

import (
	"errors"
	"fmt"

	"github.com/keshon/melodix/internal/command/music/common"
	"github.com/keshon/melodix/internal/discord"
	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/discord/reply"
	musicplayer "github.com/keshon/melodix/pkg/music/player"
)

type Next struct {
	Bot discord.VoiceAPI
}

func (c *Next) Name() string             { return "next" }
func (c *Next) Description() string      { return "Skip to the next track" }
func (c *Next) Group() string            { return "music" }
func (c *Next) Category() string         { return "🎵 Music" }
func (c *Next) UserPermissions() []int64 { return []int64{} }

func (c *Next) SlashDefinition() *cmdadapter.SlashCommand {
	return &cmdadapter.SlashCommand{
		Name:        c.Name(),
		Description: c.Description(),
	}
}

func (c *Next) Run(ctx interface{}) error {
	slashCtx, ok := ctx.(*cmdadapter.SlashInteractionContext)
	if !ok {
		return nil
	}

	guildID := slashCtx.GuildID()

	if err := slashCtx.Defer(); err != nil {
		return fmt.Errorf("failed to defer response: %w", err)
	}

	voiceState, err := c.Bot.FindUserVoiceState(guildID, slashCtx.UserID())
	if err != nil {
		slashCtx.FollowupEphemeral(&cmdadapter.Embed{
			Title:       "🎵 Voice Channel Error",
			Description: fmt.Sprintf("Join a voice channel first.\n\n**Error:** %v", err),
		})
		return nil
	}

	permOK, err := slashCtx.CanJoinVoice(voiceState.ChannelID)
	if err != nil || !permOK {
		slashCtx.FollowupEphemeral(&cmdadapter.Embed{
			Title:       "🎵 Voice Error",
			Description: "I don't have permission to join or speak in that voice channel.",
		})
		return nil
	}

	c.Bot.SetGuildMusicNotifyChannel(guildID, slashCtx.ChannelID())

	player := c.Bot.GetOrCreatePlayer(guildID)
	if player == nil {
		slashCtx.FollowupEphemeral(&cmdadapter.Embed{
			Title:       "🎵 Error",
			Description: "Music service is not available.",
		})
		return nil
	}
	queue := player.Queue()
	if len(queue) == 0 {
		slashCtx.FollowupEphemeral(&cmdadapter.Embed{
			Title:       "🎵 Queue Empty",
			Description: "No tracks left to skip.",
		})
		return nil
	}

	_ = player.Stop(false)
	if err = player.PlayNext(voiceState.ChannelID); err != nil {
		if errors.Is(err, musicplayer.ErrTrackStartFailed) {
			slashCtx.FollowupEphemeral(&cmdadapter.Embed{
				Title:       "🎵 Playback Error",
				Description: common.PlaybackErrorDescription(err),
				Color:       reply.EmbedColor,
			})
			return nil
		}
		slashCtx.FollowupEphemeral(&cmdadapter.Embed{
			Title:       "🎵 Playback Error",
			Description: fmt.Sprintf("Failed to play next track.\n\n**Error:** %v", err),
		})
		return nil
	}

	// The skip outcome is known here, so render it synchronously (async
	// transitions are handled by the voice service's status watcher).
	if track, ok := player.CurrentTrack(); ok {
		if uerr := c.Bot.AnnouncePlayback(slashCtx, guildID, reply.NowPlayingEmbed(&track)); uerr != nil {
			slashCtx.AppLog.Warn().Str("guild_id", guildID).Err(uerr).Msg("guild_status_update_failed")
		}
	}
	return nil
}
