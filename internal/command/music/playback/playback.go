// Package playback holds the voice-channel handshake and the start-playback
// flow shared by every music command that puts tracks into a guild's queue.
// Keeping it in one place is what stops /play and /search from drifting into
// two subtly different sets of checks and error wordings.
package playback

import (
	"errors"
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog"

	"github.com/keshon/melodix/internal/command/music/common"
	"github.com/keshon/melodix/internal/discord"
	"github.com/keshon/melodix/internal/discord/perm"
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
func Join(bot discord.VoiceAPI, s *discordgo.Session, e *discordgo.InteractionCreate) (Target, bool) {
	guildID := e.GuildID

	voiceState, err := bot.FindUserVoiceState(guildID, e.Member.User.ID)
	if err != nil {
		reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 Voice Error",
			Description: fmt.Sprintf("%v", err),
		})
		return Target{}, false
	}

	permOK, err := perm.CheckBotVoicePermissions(s, voiceState.ChannelID)
	if err != nil || !permOK {
		reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 Voice Error",
			Description: "I don't have permission to join or speak in that voice channel.",
		})
		return Target{}, false
	}

	// Where async playback failures should be announced later.
	bot.SetGuildMusicNotifyChannel(guildID, e.ChannelID)

	p := bot.GetOrCreatePlayer(guildID)
	if p == nil {
		reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
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
func StartAndRender(bot discord.VoiceAPI, s *discordgo.Session, e *discordgo.InteractionCreate, log zerolog.Logger, t Target, added int) {
	started := false
	if !t.Player.IsPlaying() {
		if err := t.Player.PlayNext(t.ChannelID); err != nil {
			renderStartError(s, e, err)
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
	if err := bot.UpdatePlaybackStatus(s, e, t.GuildID, embed); err != nil {
		log.Warn().Str("guild_id", t.GuildID).Err(err).Msg("guild_status_update_failed")
	}
}

// QueueError reports a failed enqueue.
func QueueError(s *discordgo.Session, e *discordgo.InteractionCreate, err error) {
	reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
		Title:       "🎵 Queue Error",
		Description: fmt.Sprintf("%v", err),
	})
}

func renderStartError(s *discordgo.Session, e *discordgo.InteractionCreate, err error) {
	switch {
	case errors.Is(err, player.ErrTrackStartFailed):
		reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 Playback Error",
			Description: common.PlaybackErrorDescription(err),
			Color:       reply.EmbedColor,
		})
	case errors.Is(err, player.ErrNoTracksInQueue):
		reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 Queue",
			Description: "Nothing is in the queue to play.",
			Color:       reply.EmbedColor,
		})
	default:
		reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 Playback Error",
			Description: fmt.Sprintf("%v", err),
			Color:       reply.EmbedColor,
		})
	}
}
