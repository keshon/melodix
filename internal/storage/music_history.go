package storage

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/keshon/melodix/internal/domain"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/sources"
)

// Default cap for persisted playback rows per guild (trim oldest on append).
var musicPlaybackHistoryLimit = 750

// ErrMusicPlaybackNotFound is returned when no row matches the id (unknown, trimmed, or typo).
var ErrMusicPlaybackNotFound = errors.New("music playback not found")

func musicPlaybackFromTrack(id uint64, at time.Time, tp parsers.Track) domain.MusicPlayback {
	return domain.MusicPlayback{
		ID:               id,
		PlayedAt:         at,
		URL:              tp.URL,
		Title:            tp.Title,
		CurrentParser:    tp.CurrentParser,
		AvailableParsers: slices.Clone(tp.SourceInfo.AvailableParsers),
		SourceName:       tp.SourceInfo.SourceName,
	}
}

// TrackInfoFromMusicPlayback rebuilds resolver metadata for enqueue. Current parser is first in AvailableParsers when possible.
func TrackInfoFromMusicPlayback(m domain.MusicPlayback) sources.TrackInfo {
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

// AppendMusicPlayback assigns a monotonic id, appends, trims oldest rows, and persists.
func (s *Storage) AppendMusicPlayback(guildID string, track parsers.Track, at time.Time) (uint64, error) {
	record, err := s.getOrCreateGuildRecord(guildID)
	if err != nil {
		return 0, err
	}

	record.NextMusicHistoryID++
	id := record.NextMusicHistoryID
	row := musicPlaybackFromTrack(id, at, track)
	record.MusicPlaybackHistory = append(record.MusicPlaybackHistory, row)

	if len(record.MusicPlaybackHistory) > musicPlaybackHistoryLimit {
		record.MusicPlaybackHistory = record.MusicPlaybackHistory[len(record.MusicPlaybackHistory)-musicPlaybackHistoryLimit:]
	}

	if err := s.ds.Set(guildID, record); err != nil {
		return 0, fmt.Errorf("persist music playback: %w", err)
	}
	return id, nil
}

// MusicPlayback returns one row by id.
func (s *Storage) MusicPlayback(guildID string, id uint64) (domain.MusicPlayback, error) {
	record, err := s.getOrCreateGuildRecord(guildID)
	if err != nil {
		return domain.MusicPlayback{}, err
	}
	for _, row := range record.MusicPlaybackHistory {
		if row.ID == id {
			return row, nil
		}
	}
	return domain.MusicPlayback{}, ErrMusicPlaybackNotFound
}

// ListMusicPlaybackTimeline returns persisted rows oldest-first (chronological).
func (s *Storage) ListMusicPlaybackTimeline(guildID string) ([]domain.MusicPlayback, error) {
	record, err := s.getOrCreateGuildRecord(guildID)
	if err != nil {
		return nil, err
	}
	return slices.Clone(record.MusicPlaybackHistory), nil
}
