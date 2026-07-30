// Package storage persists guild settings, the command log, playback history
// and the track-cache index in an embedded write-ahead-logged datastore.
package storage

import (
	"time"

	"github.com/keshon/datastore"
	"github.com/rs/zerolog"
)

// Retention caps, applied by trimming the oldest rows on append.
const commandHistoryLimit = 50

// musicPlaybackHistoryLimit is a var so tests can shrink it.
var musicPlaybackHistoryLimit = 750

// Storage owns the database and the collections declared on it. Every
// collection and index must be registered before Open, so construction is the
// only place the schema is described.
type Storage struct {
	db  *datastore.DB
	log zerolog.Logger

	settings *datastore.Collection[*GuildSettings]
	cmdLog   *datastore.Collection[*CommandLogEntry]
	playback *datastore.Collection[*PlaybackEntry]
	cacheIdx *datastore.Collection[*CacheEntry]

	cmdLogByGuild   *datastore.Index[*CommandLogEntry]
	playbackByGuild *datastore.Index[*PlaybackEntry]
}

// NewStorage opens the database in dir, creating it if needed. The directory is
// locked for the lifetime of the process: a second process opening the same dir
// fails with datastore.ErrLocked.
func NewStorage(dir string, log zerolog.Logger) (*Storage, error) {
	db := datastore.New(datastore.Options{Dir: dir, Logger: &log})
	s := &Storage{db: db, log: log}

	s.settings = datastore.Register[*GuildSettings](db, "guild_settings")
	s.cmdLog = datastore.Register[*CommandLogEntry](db, "command_log")
	s.playback = datastore.Register[*PlaybackEntry](db, "playback")
	s.cacheIdx = datastore.Register[*CacheEntry](db, "cache_entries")

	s.cmdLogByGuild = datastore.AddIndex(s.cmdLog, "guild",
		func(c *CommandLogEntry) []string { return []string{c.GuildID} })
	s.playbackByGuild = datastore.AddIndex(s.playback, "guild",
		func(p *PlaybackEntry) []string { return []string{p.GuildID} })

	if err := db.Open(); err != nil {
		return nil, err
	}
	return s, nil
}

// Close compacts and releases the directory lock. Safe to call twice.
func (s *Storage) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// guildSettings returns the guild's settings, or a blank set for an unknown
// guild. The returned value is a private copy: mutate it and Put it back.
func (s *Storage) guildSettings(guildID string) *GuildSettings {
	if g, ok := s.settings.Get(guildID); ok {
		return g
	}
	return &GuildSettings{GuildID: guildID}
}

// trimOldest deletes the oldest rows so that appending one more leaves at most
// limit rows. existing must be in key order (which is chronological).
func trimOldest[T datastore.Entity](col *datastore.TxCollection[T], existing []T, limit int) error {
	over := len(existing) + 1 - limit
	for i := 0; i < over && i < len(existing); i++ {
		if err := col.Delete(existing[i].Key()); err != nil {
			return err
		}
	}
	return nil
}

// SetCommand records one command invocation, trimming the guild's oldest.
func (s *Storage) SetCommand(
	guildID, channelID, channelName, guildName, userID, username, command string,
) error {
	entry := &CommandLogEntry{
		GuildID:     guildID,
		ChannelID:   channelID,
		ChannelName: channelName,
		GuildName:   guildName,
		UserID:      userID,
		Username:    username,
		Command:     command,
		Datetime:    time.Now(),
	}
	return s.db.Update(func(tx *datastore.Tx) error {
		entry.ID = tx.NextID("cmdlog:" + guildID)
		col := datastore.In(tx, s.cmdLog)
		if err := col.Put(entry); err != nil {
			return err
		}
		// Read the index inside the transaction: the rows we trim against are
		// then the rows the commit sees, not a snapshot from before the writer
		// slot was ours. (The entry staged above is not indexed until commit,
		// so `existing` is the guild's rows without it — which is what the
		// limit arithmetic in trimOldest expects.)
		existing := datastore.InIndex(tx, s.cmdLogByGuild).Find(guildID)
		return trimOldest(col, existing, commandHistoryLimit)
	})
}

// CommandHistory returns the guild's recorded commands, oldest first.
func (s *Storage) CommandHistory(guildID string) ([]CommandLogEntry, error) {
	rows := s.cmdLogByGuild.Find(guildID)
	out := make([]CommandLogEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, *r)
	}
	return out, nil
}

// GuildExport is the shape served by the maintenance database dump.
type GuildExport struct {
	GuildID          string            `json:"guild_id"`
	CommandsDisabled []string          `json:"commands_disabled"`
	CommandsHistory  []CommandLogEntry `json:"commands_history"`
	PlaybackHistory  []PlaybackEntry   `json:"playback_history"`
}

// ExportGuild gathers everything stored for one guild.
func (s *Storage) ExportGuild(guildID string) (GuildExport, error) {
	cmds, err := s.CommandHistory(guildID)
	if err != nil {
		return GuildExport{}, err
	}
	plays, err := s.ListMusicPlaybackTimeline(guildID)
	if err != nil {
		return GuildExport{}, err
	}
	return GuildExport{
		GuildID:          guildID,
		CommandsDisabled: s.guildSettings(guildID).CommandsDisabled,
		CommandsHistory:  cmds,
		PlaybackHistory:  plays,
	}, nil
}
