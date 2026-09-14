// Package playback holds the voice-channel handshake and the start-playback
// flow shared by every music command that puts tracks into a guild's queue.
// Keeping it in one place is what stops /play and /search from drifting into
// two subtly different sets of checks and error wordings.
package playback

import (
	"errors"
	"fmt"

	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/rs/zerolog"

	"github.com/keshon/melodix/internal/command/music/common"
	"github.com/keshon/melodix/internal/discord"
	"github.com/keshon/melodix/internal/discord/reply"
	"github.com/keshon/melodix/pkg/music/player"
)

// Target is a validated place to play: the guild's player plus the voice
// channel the requesting member is sitting in.
type Target struct {
	Player    *player.Player
	ChannelID string
	GuildID   string
}

// Join checks that the invoking member is in a voice channel the bot may join
// and returns the guild's player. When it cannot, it has already answered the
// interaction with the reason and reports ok=false, so callers just return.
//
// The interaction must already be deferred: every reply here is a followup.
func Join(bot discord.VoiceAPI, ctx cmdadapter.Interaction) (Target, bool) {
	guildID := ctx.GuildID()

	voiceState, err := bot.FindUserVoiceState(guildID, ctx.UserID())
	if err != nil {
		ctx.FollowupEphemeral(&cmdadapter.Embed{
			Title:       "🎵 Voice Error",
			Description: fmt.Sprintf("%v", err),
		})
		return Target{}, false
	}

	permOK, err := ctx.CanJoinVoice(voiceState.ChannelID)
	if err != nil || !permOK {
		ctx.FollowupEphemeral(&cmdadapter.Embed{
			Title:       "🎵 Voice Error",
			Description: "I don't have permission to join or speak in that voice channel.",
		})
		return Target{}, false
	}

	// Where async playback failures should be announced later.
	bot.SetGuildMusicNotifyChannel(guildID, ctx.ChannelID())

	p := bot.GetOrCreatePlayer(guildID)
	if p == nil {
		ctx.FollowupEphemeral(&cmdadapter.Embed{
			Title:       "🎵 Error",
			Description: "Music service is not available.",
		})
		return Target{}, false
	}

	return Target{Player: p, ChannelID: voiceState.ChannelID, GuildID: guildID}, true
}

// StartAndRender starts playback when the player is idle, then renders the
// outcome into the guild's music status message. added is how many tracks the
// caller just queued, which is what the reply reports when something was
// already playing.
//
// The outcome is known here, so it is rendered synchronously; asynchronous
// transitions such as auto-advance and queue end belong to the voice service's
// status watcher instead.
func StartAndRender(bot discord.VoiceAPI, ctx cmdadapter.Interaction, log zerolog.Logger, t Target, added int) {
	started := false
	if !t.Player.IsPlaying() {
		if err := t.Player.PlayNext(t.ChannelID); err != nil {
			renderStartError(ctx, err)
			return
		}
		started = true
	}

	embed := reply.TracksAddedEmbed(added)
	if started {
		if track := t.Player.CurrentTrack(); track != nil {
			embed = reply.NowPlayingEmbed(track)
		}
	}
	// The status message outlives the interaction token, so the voice service
	// needs the interaction itself. That is the last signature holding a
	// session and an event; see cmdadapter.Interaction.Raw.
	rawSession, rawEvent := ctx.Raw()
	if err := bot.UpdatePlaybackStatus(rawSession, rawEvent, t.GuildID, embed); err != nil {
		log.Warn().Str("guild_id", t.GuildID).Err(err).Msg("guild_status_update_failed")
	}
}

// QueueError reports a failed enqueue.
func QueueError(ctx cmdadapter.Interaction, err error) {
	ctx.FollowupEphemeral(&cmdadapter.Embed{
		Title:       "🎵 Queue Error",
		Description: fmt.Sprintf("%v", err),
	})
}

func renderStartError(ctx cmdadapter.Interaction, err error) {
	switch {
	case errors.Is(err, player.ErrTrackStartFailed):
		ctx.FollowupEphemeral(&cmdadapter.Embed{
			Title:       "🎵 Playback Error",
			Description: common.PlaybackErrorDescription(err),
			Color:       reply.EmbedColor,
		})
	case errors.Is(err, player.ErrNoTracksInQueue):
		ctx.FollowupEphemeral(&cmdadapter.Embed{
			Title:       "🎵 Queue",
			Description: "Nothing is in the queue to play.",
			Color:       reply.EmbedColor,
		})
	default:
		ctx.FollowupEphemeral(&cmdadapter.Embed{
			Title:       "🎵 Playback Error",
			Description: fmt.Sprintf("%v", err),
			Color:       reply.EmbedColor,
		})
	}
}
