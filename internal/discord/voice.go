package discord

import "github.com/keshon/melodix/internal/music/player"

// BotVoice is the interface the Discord bot provides for voice/music.
type BotVoice interface {
	GetOrCreatePlayer(guildID string) *player.Player
	FindUserVoiceState(guildID, userID string) (*VoiceState, error)
}

// VoiceState holds minimal voice channel state for a user.
type VoiceState struct {
	ChannelID string
	UserID    string
}
