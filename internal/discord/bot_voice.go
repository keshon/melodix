package discord

import (
	"fmt"

	"github.com/keshon/melodix/internal/discord/adapter"
	"github.com/keshon/melodix/internal/discord/voice"
	"github.com/keshon/melodix/internal/music"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/player"
)

// VoiceAPI is the interface the Discord bot exposes for voice/music commands.
type VoiceAPI interface {
	// FindUserVoiceState returns the voice channel a user is currently in, or an
	// error if none.
	FindUserVoiceState(guildID, userID string) (*UserVoiceState, error)

	// Music is the service the guilds' players live in, or nil when the bot
	// runs without voice.
	Music() *music.Service

	// PlayNextAndAnnounce starts the next queued track and makes the answer
	// to the interaction the guild's playback status message, so the
	// asynchronous transitions can keep editing it past the token's expiry.
	// skipped is the track /next is moving past, or nil.
	PlayNextAndAnnounce(to adapter.Interaction, p *player.Player, guildID, voiceChannelID string, added int, skipped *parsers.Track) error

	// SetGuildMusicNotifyChannel stores the slash command text channel for async
	// playback failure UI.
	SetGuildMusicNotifyChannel(guildID, channelID string)
}

// ErrAnnounceFailed is voice.ErrAnnounceFailed, so a command can tell a track
// that did not start from one that started and was not announced.
var ErrAnnounceFailed = voice.ErrAnnounceFailed

// UserVoiceState holds minimal voice channel state for a user. Aliased from
// the voice service so a caller keeps naming it discord.UserVoiceState.
type UserVoiceState = voice.UserVoiceState

// FindUserVoiceState returns the voice channel a user is currently in, or an
// error if none (delegates to voice service).
//
// This used to read b.dg directly, and it is the one VoiceAPI method that did.
// b.dg is written under b.mu when a session restarts and this runs on the
// command path, so the two raced; the voice service reads the session through
// the getter that takes the lock.
func (b *Bot) FindUserVoiceState(guildID, userID string) (*UserVoiceState, error) {
	if b.voice == nil {
		return nil, fmt.Errorf("voice service not available")
	}
	return b.voice.FindUserVoiceState(guildID, userID)
}

// Music is the service the guilds' players live in.
func (b *Bot) Music() *music.Service {
	return b.music
}

// PlayNextAndAnnounce starts the next track and registers the answer as the
// guild's playback status message (delegates to voice service).
func (b *Bot) PlayNextAndAnnounce(to adapter.Interaction, p *player.Player, guildID, voiceChannelID string, added int, skipped *parsers.Track) error {
	if b.voice == nil {
		return fmt.Errorf("voice service not available")
	}
	return b.voice.PlayNextAndAnnounce(to, p, guildID, voiceChannelID, added, skipped)
}

// SetGuildMusicNotifyChannel records the text channel for public
// playback-failure fallback (voice service).
func (b *Bot) SetGuildMusicNotifyChannel(guildID, channelID string) {
	if b.voice == nil {
		return
	}
	b.voice.SetGuildMusicNotifyChannel(guildID, channelID)
}
