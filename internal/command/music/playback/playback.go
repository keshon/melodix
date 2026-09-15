// Package playback holds the voice-channel handshake and the start-playback
// flow shared by every music command that puts tracks into a guild's queue.
// Keeping it in one place is what stops /play and /search from drifting into
// two subtly different sets of checks and error wordings.
package playback

import (
	"errors"
	"fmt"

	"github.com/keshon/melodix/internal/discord/adapter"
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
func Join(bot discord.VoiceAPI, ctx adapter.Interaction) (Target, bool) {
	guildID := ctx.GuildID()

	voiceState, err := bot.FindUserVoiceState(guildID, ctx.UserID())
	if err != nil {
		ctx.FollowupEphemeral(&adapter.Embed{
			Title:       "🎵 Voice Error",
			Description: fmt.Sprintf("%v", err),
		})
		return Target{}, false
	}

	permOK, err := ctx.CanJoinVoice(voiceState.ChannelID)
	if err != nil || !permOK {
		ctx.FollowupEphemeral(&adapter.Embed{
			Title:       "🎵 Voice Error",
			Description: "I don't have permission to join or speak in that voice channel.",
		})
		return Target{}, false
	}

	// Where async playback failures should be announced later.
	bot.SetGuildMusicNotifyChannel(guildID, ctx.ChannelID())

	p := bot.GetOrCreatePlayer(guildID)
	if p == nil {
		ctx.FollowupEphemeral(&adapter.Embed{
			Title:       "🎵 Error",
			Description: "Music service is not available.",
		})
		return Target{}, false
	}

	return Target{Player: p, ChannelID: voiceState.ChannelID, GuildID: guildID}, true
}

// StartAndRender starts playback when the player is idle, then answers the
// caller with what happened. added is how many tracks the caller just queued,
// which is what the answer reports when something was already playing.
//
// The caller is answered either way, and the two cases differ in what the
// answer means rather than in whether there is one:
//
//   - Playback started here, so the answer is what is now playing, and it
//     becomes the guild's status message for the asynchronous transitions --
//     auto-advance, queue end -- to edit afterwards.
//   - Something was already playing, so the answer is what happened to the
//     caller's tracks. The status message is left alone: it is showing what
//     is playing, which has not changed, and overwriting it with "added"
//     would replace the answer to "what is on" with the answer to a question
//     nobody asked twice.
func StartAndRender(bot discord.VoiceAPI, ctx adapter.Interaction, log zerolog.Logger, t Target, added int) {
	if t.Player.IsPlaying() {
		if err := ctx.Respond(reply.TracksAddedEmbed(added)); err != nil {
			log.Warn().Str("guild_id", t.GuildID).Err(err).Msg("queue_added_reply_failed")
		}
		return
	}

	if err := t.Player.PlayNext(t.ChannelID); err != nil {
		renderStartError(ctx, err)
		return
	}

	embed := reply.TracksAddedEmbed(added)
	if track, ok := t.Player.CurrentTrack(); ok {
		embed = reply.NowPlayingEmbed(&track)
	}
	if err := bot.AnnouncePlayback(ctx, t.GuildID, embed); err != nil {
		log.Warn().Str("guild_id", t.GuildID).Err(err).Msg("guild_status_update_failed")
	}
}

// QueueError reports a failed enqueue.
func QueueError(ctx adapter.Interaction, err error) {
	ctx.FollowupEphemeral(&adapter.Embed{
		Title:       "🎵 Queue Error",
		Description: fmt.Sprintf("%v", err),
	})
}

func renderStartError(ctx adapter.Interaction, err error) {
	switch {
	case errors.Is(err, player.ErrTrackStartFailed):
		ctx.FollowupEphemeral(&adapter.Embed{
			Title:       "🎵 Playback Error",
			Description: common.PlaybackErrorDescription(err),
			Color:       reply.EmbedColor,
		})
	case errors.Is(err, player.ErrNoTracksInQueue):
		ctx.FollowupEphemeral(&adapter.Embed{
			Title:       "🎵 Queue",
			Description: "Nothing is in the queue to play.",
			Color:       reply.EmbedColor,
		})
	default:
		ctx.FollowupEphemeral(&adapter.Embed{
			Title:       "🎵 Playback Error",
			Description: fmt.Sprintf("%v", err),
			Color:       reply.EmbedColor,
		})
	}
}
