package discord

import (
	"fmt"

	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/discord/voice"
	"github.com/keshon/melodix/pkg/music/player"
	"github.com/keshon/melodix/pkg/music/sources"
)

// VoiceAPI is the interface the Discord bot exposes for voice/music commands.
type VoiceAPI interface {
	// GetOrCreatePlayer returns an existing player for the guild or creates a new
	// one.
	GetOrCreatePlayer(guildID string) *player.Player

	// FindUserVoiceState returns the voice channel a user is currently in, or an
	// error if none.
	FindUserVoiceState(guildID, userID string) (*UserVoiceState, error)

	// Resolve resolves input to tracks using the bot's shared resolver.
	ResolveTracks(guildID, input, source, parser string) ([]sources.TrackInfo, error)

	// AnnouncePlayback answers the interaction with embed and makes that
	// message the guild's playback status message, so the asynchronous
	// transitions can keep editing it past the token's expiry.
	AnnouncePlayback(to cmdadapter.Interaction, guildID string, embed *cmdadapter.Embed) error

	// SetGuildMusicNotifyChannel stores the slash command text channel for async
	// playback failure UI.
	SetGuildMusicNotifyChannel(guildID, channelID string)
}

// UserVoiceState holds minimal voice channel state for a user. Aliased from
// the voice service so a caller keeps naming it discord.UserVoiceState.
type UserVoiceState = voice.UserVoiceState

// GetOrCreatePlayer returns an existing player for the guild or creates a new
// one (delegates to voice service).
func (b *Bot) GetOrCreatePlayer(guildID string) *player.Player {
	if b.voice == nil {
		return nil
	}
	return b.voice.GetOrCreatePlayer(guildID)
}

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

// ResolveTracks resolves input to tracks using the bot's shared resolver
// (delegates to voice service).
func (b *Bot) ResolveTracks(guildID, input, source, parser string) ([]sources.TrackInfo, error) {
	if b.voice == nil {
		return nil, fmt.Errorf("voice service not available")
	}
	return b.voice.ResolveTracks(guildID, input, source, parser)
}

// AnnouncePlayback answers the interaction and registers the reply as the
// guild's playback status message (delegates to voice service).
func (b *Bot) AnnouncePlayback(to cmdadapter.Interaction, guildID string, embed *cmdadapter.Embed) error {
	if b.voice == nil {
		return nil
	}
	return b.voice.AnnouncePlayback(to, guildID, embed)
}

// SetGuildMusicNotifyChannel records the text channel for public
// playback-failure fallback (voice service).
func (b *Bot) SetGuildMusicNotifyChannel(guildID, channelID string) {
	if b.voice == nil {
		return
	}
	b.voice.SetGuildMusicNotifyChannel(guildID, channelID)
}
