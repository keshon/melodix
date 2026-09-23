package next

import (
	"errors"
	"fmt"

	"github.com/keshon/melodix/internal/discord"
	"github.com/keshon/melodix/internal/discord/adapter"
	"github.com/keshon/melodix/internal/discord/reply"
	"github.com/keshon/melodix/pkg/music/parsers"
	musicplayer "github.com/keshon/melodix/pkg/music/player"
)

type Command struct {
	Bot discord.VoiceAPI
}

func (c *Command) Name() string             { return "next" }
func (c *Command) Description() string      { return "Skip to the next track" }
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

	guildID := slashCtx.GuildID()

	if err := slashCtx.Defer(); err != nil {
		return fmt.Errorf("failed to defer response: %w", err)
	}

	voiceState, err := c.Bot.FindUserVoiceState(guildID, slashCtx.UserID())
	if err != nil {
		slashCtx.FollowupEphemeral(&adapter.Embed{
			Title:       "🎵 Voice Channel Error",
			Description: fmt.Sprintf("Join a voice channel first.\n\n**Error:** %v", err),
		})
		return nil
	}

	permOK, err := slashCtx.CanJoinVoice(voiceState.ChannelID)
	if err != nil || !permOK {
		slashCtx.FollowupEphemeral(&adapter.Embed{
			Title:       "🎵 Voice Error",
			Description: "I don't have permission to join or speak in that voice channel.",
		})
		return nil
	}

	c.Bot.SetGuildMusicNotifyChannel(guildID, slashCtx.ChannelID())

	player := c.Bot.GetOrCreatePlayer(guildID)
	if player == nil {
		slashCtx.FollowupEphemeral(&adapter.Embed{
			Title:       "🎵 Error",
			Description: "Music service is not available.",
		})
		return nil
	}
	queue := player.Queue()
	if len(queue) == 0 {
		slashCtx.FollowupEphemeral(&adapter.Embed{
			Title:       "🎵 Queue Empty",
			Description: "No tracks left to skip.",
		})
		return nil
	}

	skipped, wasPlaying := player.CurrentTrack()
	_ = player.Stop(false)
	var moving *parsers.Track
	if wasPlaying {
		moving = &skipped
	}
	err = c.Bot.PlayNextAndAnnounce(slashCtx, player, guildID, voiceState.ChannelID, 0, moving)
	if errors.Is(err, discord.ErrAnnounceFailed) {
		slashCtx.AppLog.Warn().Str("guild_id", guildID).Err(err).Msg("guild_status_update_failed")
		return nil
	}
	if err != nil {
		if errors.Is(err, musicplayer.ErrTrackStartFailed) {
			slashCtx.FollowupEphemeral(&adapter.Embed{
				Title:       "🎵 Playback Error",
				Description: reply.ClampEmbedText(err.Error()),
				Color:       reply.EmbedColor,
			})
			return nil
		}
		slashCtx.FollowupEphemeral(&adapter.Embed{
			Title:       "🎵 Playback Error",
			Description: fmt.Sprintf("Failed to play next track.\n\n**Error:** %v", err),
		})
		return nil
	}
	return nil
}
