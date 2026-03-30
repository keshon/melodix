package storage

import (
	"fmt"
	"slices"
	"time"

	st "github.com/keshon/melodix/internal/domain"
)

// ErrMusicPlaybackNotFound is returned when no row matches the id (unknown, trimmed, or typo).
var ErrMusicPlaybackNotFound = st.ErrMusicPlaybackNotFound

func musicPlaybackFromAppend(id uint64, at time.Time, rec st.MusicPlaybackAppend) st.MusicPlayback {
	return st.MusicPlayback{
		ID:               id,
		PlayedAt:         at,
		URL:              rec.URL,
		Title:            rec.Title,
		CurrentParser:    rec.CurrentParser,
		AvailableParsers: slices.Clone(rec.AvailableParsers),
		SourceName:       rec.SourceName,
	}
}

// AppendMusicPlayback assigns a monotonic id, appends, trims oldest rows, and persists.
func (s *Storage) AppendMusicPlayback(guildID string, at time.Time, rec st.MusicPlaybackAppend) (uint64, error) {
	record, err := s.getOrCreateGuildRecord(guildID)
	if err != nil {
		return 0, err
	}

	record.NextMusicHistoryID++
	id := record.NextMusicHistoryID
	row := musicPlaybackFromAppend(id, at, rec)
	record.MusicPlaybackHistory = append(record.MusicPlaybackHistory, row)

	lim := s.musicPlaybackHistoryLimit
	if len(record.MusicPlaybackHistory) > lim {
		record.MusicPlaybackHistory = record.MusicPlaybackHistory[len(record.MusicPlaybackHistory)-lim:]
	}

	if err := s.ds.Set(guildID, record); err != nil {
		return 0, fmt.Errorf("persist music playback: %w", err)
	}
	return id, nil
}

// GetMusicPlayback returns one row by id.
func (s *Storage) GetMusicPlayback(guildID string, id uint64) (st.MusicPlayback, error) {
	record, err := s.getOrCreateGuildRecord(guildID)
	if err != nil {
		return st.MusicPlayback{}, err
	}
	for _, row := range record.MusicPlaybackHistory {
		if row.ID == id {
			return row, nil
		}
	}
	return st.MusicPlayback{}, st.ErrMusicPlaybackNotFound
}

// ListMusicPlaybackTimeline returns persisted rows oldest-first (chronological).
func (s *Storage) ListMusicPlaybackTimeline(guildID string) ([]st.MusicPlayback, error) {
	record, err := s.getOrCreateGuildRecord(guildID)
	if err != nil {
		return nil, err
	}
	return slices.Clone(record.MusicPlaybackHistory), nil
}

// ListMusicPlaybackTimelinePage returns a page of persisted rows oldest-first (chronological) plus total count.
func (s *Storage) ListMusicPlaybackTimelinePage(guildID string, offset, limit int) ([]st.MusicPlayback, int, error) {
	record, err := s.getOrCreateGuildRecord(guildID)
	if err != nil {
		return nil, 0, err
	}
	total := len(record.MusicPlaybackHistory)
	if limit <= 0 || total == 0 {
		return nil, total, nil
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return []st.MusicPlayback{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return slices.Clone(record.MusicPlaybackHistory[offset:end]), total, nil
}
