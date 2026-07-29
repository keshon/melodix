package storage

import (
	"fmt"
	"time"

	"github.com/keshon/melodix/pkg/music/cache"
)

// Persisted record types. Each satisfies datastore.Entity via Key(), which is
// what addresses the record in the write-ahead log.
//
// Per-guild rows embed their guild id and key on "<guildID>:<zero-padded id>":
// the padding makes lexicographic key order equal chronological id order, which
// is what lets an index read return history oldest-first without re-sorting.

// guildRowKey builds the ordered composite key for a per-guild numbered row.
func guildRowKey(guildID string, id uint64) string {
	return fmt.Sprintf("%s:%020d", guildID, id)
}

// GuildSettings holds per-guild configuration (currently disabled command groups).
type GuildSettings struct {
	GuildID          string   `json:"guild_id"`
	CommandsDisabled []string `json:"commands_disabled"`
}

func (g *GuildSettings) Key() string { return g.GuildID }

// CommandLogEntry is one recorded command invocation.
type CommandLogEntry struct {
	ID          uint64    `json:"id"`
	GuildID     string    `json:"guild_id"`
	ChannelID   string    `json:"channel_id"`
	ChannelName string    `json:"channel_name"`
	GuildName   string    `json:"guild_name"`
	UserID      string    `json:"user_id"`
	Username    string    `json:"username"`
	Command     string    `json:"command"`
	Datetime    time.Time `json:"datetime"`
}

func (c *CommandLogEntry) Key() string { return guildRowKey(c.GuildID, c.ID) }

// PlaybackEntry is one persisted row for a track that actually started playing.
// The id is per guild and monotonic; /play <id> replays it.
type PlaybackEntry struct {
	ID               uint64    `json:"id"`
	GuildID          string    `json:"guild_id"`
	PlayedAt         time.Time `json:"played_at"`
	URL              string    `json:"url"`
	Title            string    `json:"title"`
	CurrentParser    string    `json:"current_parser"`
	AvailableParsers []string  `json:"available_parsers"`
	SourceName       string    `json:"source_name"`
}

func (p *PlaybackEntry) Key() string { return guildRowKey(p.GuildID, p.ID) }

// CacheEntry persists one track-cache index row. It embeds the cache package's
// own record so the two never drift; cache.Entry names its content key ID
// precisely so this Key() method can exist.
type CacheEntry struct {
	cache.Entry
}

func (c *CacheEntry) Key() string { return c.ID }
