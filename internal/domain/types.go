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

// MusicPlaybackAppend is metadata to append as a new history row (id and played time assigned by storage).
type MusicPlaybackAppend struct {
	URL              string
	Title            string
	CurrentParser    string
	AvailableParsers []string
	SourceName       string
}

type Record struct {
	CommandsDisabled []string          `json:"commands_disabled"`
	CommandsHistory  []CommandHistory  `json:"commands_history"`
	CommandHashes    map[string]string `json:"command_hashes,omitempty"` // slash command name -> hash for sync

	MusicPlaybackHistory []MusicPlayback `json:"music_playback_history,omitempty"`
	NextMusicHistoryID   uint64          `json:"next_music_history_id"`
}

// MusicHistoryRepository persists and reads per-guild playback history (domain rows only).
type MusicHistoryRepository interface {
	GetMusicPlayback(guildID string, id uint64) (MusicPlayback, error)
	ListMusicPlaybackTimeline(guildID string) ([]MusicPlayback, error)
	// ListMusicPlaybackTimelinePage returns a window of chronological rows and the total available rows.
	// offset is 0-based. If limit <= 0, rows may be empty but total must still be returned.
	ListMusicPlaybackTimelinePage(guildID string, offset, limit int) (rows []MusicPlayback, total int, err error)
	AppendMusicPlayback(guildID string, at time.Time, rec MusicPlaybackAppend) (uint64, error)
}
