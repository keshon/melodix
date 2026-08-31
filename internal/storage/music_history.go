package storage

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/keshon/datastore"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/sources"
)

// ErrMusicPlaybackNotFound is returned when no row matches the id (unknown,
// trimmed, or typo).
var ErrMusicPlaybackNotFound = errors.New("music playback not found")

func playbackFromTrack(id uint64, guildID string, at time.Time, tp parsers.Track) *PlaybackEntry {
	return &PlaybackEntry{
		ID:               id,
		GuildID:          guildID,
		PlayedAt:         at,
		URL:              tp.URL,
		Title:            tp.Title,
		CurrentParser:    tp.CurrentParser,
		AvailableParsers: slices.Clone(tp.SourceInfo.AvailableParsers),
		SourceName:       tp.SourceInfo.SourceName,
	}
}

// TrackInfoFromMusicPlayback rebuilds resolver metadata for enqueue. Current
// parser is first in AvailableParsers when possible.
func TrackInfoFromMusicPlayback(m PlaybackEntry) sources.TrackInfo {
	parsersList := slices.Clone(m.AvailableParsers)
	if m.CurrentParser != "" {
		if i := slices.Index(parsersList, m.CurrentParser); i > 0 {
			parsersList[0], parsersList[i] = parsersList[i], parsersList[0]
		} else if i < 0 {
			parsersList = append([]string{m.CurrentParser}, parsersList...)
		}
	}
	return sources.TrackInfo{
		URL:              m.URL,
		Title:            m.Title,
		SourceName:       m.SourceName,
		AvailableParsers: parsersList,
	}
}

// AppendMusicPlayback assigns a per-guild monotonic id, stores the row and
// trims the guild's oldest rows past the retention limit.
func (s *Storage) AppendMusicPlayback(guildID string, track parsers.Track, at time.Time) (uint64, error) {
	var id uint64
	err := s.db.Update(func(tx *datastore.Tx) error {
		id = tx.NextID("playback:" + guildID)
		col := datastore.In(tx, s.playback)
		if err := col.Put(playbackFromTrack(id, guildID, at, track)); err != nil {
			return err
		}
		// Index read inside the transaction — see the note in SetCommand.
		existing := datastore.InIndex(tx, s.playbackByGuild).Find(guildID)
		return trimOldest(col, existing, musicPlaybackHistoryLimit)
	})
	if err != nil {
		return 0, fmt.Errorf("persist music playback: %w", err)
	}
	return id, nil
}

// MusicPlayback returns one row by id.
func (s *Storage) MusicPlayback(guildID string, id uint64) (PlaybackEntry, error) {
	row, ok := s.playback.Get(guildRowKey(guildID, id))
	if !ok {
		return PlaybackEntry{}, ErrMusicPlaybackNotFound
	}
	return *row, nil
}

// ListMusicPlaybackTimeline returns persisted rows oldest-first
// (chronological).
func (s *Storage) ListMusicPlaybackTimeline(guildID string) ([]PlaybackEntry, error) {
	rows := s.playbackByGuild.Find(guildID)
	out := make([]PlaybackEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, *r)
	}
	return out, nil
}
