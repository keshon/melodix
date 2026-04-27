package domain

import (
	"time"
)

type CommandHistory struct {
	ChannelID   string    `json:"channel_id"`
	ChannelName string    `json:"channel_name"`
	GuildName   string    `json:"guild_name"`
	UserID      string    `json:"user_id"`
	Username    string    `json:"username"`
	Command     string    `json:"command"`
	Datetime    time.Time `json:"datetime"`
}

// MusicPlayback is one persisted row for a track that actually started playing (Discord).
type MusicPlayback struct {
	ID               uint64    `json:"id"`
	PlayedAt         time.Time `json:"played_at"`
	URL              string    `json:"url"`
	Title            string    `json:"title"`
	CurrentParser    string    `json:"current_parser"`
	AvailableParsers []string  `json:"available_parsers"`
	SourceName       string    `json:"source_name"`
}

type Record struct {
	CommandsDisabled []string          `json:"commands_disabled"`
	CommandsHistory  []CommandHistory  `json:"commands_history"`

	MusicPlaybackHistory []MusicPlayback `json:"music_playback_history,omitempty"`
	NextMusicHistoryID   uint64          `json:"next_music_history_id"`
}
