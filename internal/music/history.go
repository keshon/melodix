package music

import (
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/keshon/melodix/internal/storage"
)

// History returns the scope's plays newest first, so the first page is what
// was just played. Storage keeps them oldest first -- its trimming relies on
// that -- so the order is turned here rather than there.
func (s *Service) History(scope string) ([]storage.PlaybackEntry, error) {
	if s.store == nil {
		return nil, ErrHistoryUnavailable
	}
	rows, err := s.store.ListMusicPlaybackTimeline(scope)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrHistoryLoad, err)
	}
	slices.Reverse(rows)
	return rows, nil
}

// PlayCount is one distinct URL in a scope's history. ID is its latest play,
// so replaying by it replays the newest entry.
type PlayCount struct {
	ID         uint64
	URL        string
	Title      string
	Count      int
	LastPlayed time.Time
}

// PlayCounts groups history by URL (string equality), most played first and
// then most recently played.
func PlayCounts(history []storage.PlaybackEntry) []PlayCount {
	byURL := make(map[string]*PlayCount)
	for _, row := range history {
		c, ok := byURL[row.URL]
		if !ok {
			byURL[row.URL] = &PlayCount{
				ID:         row.ID,
				URL:        row.URL,
				Title:      row.Title,
				Count:      1,
				LastPlayed: row.PlayedAt,
			}
			continue
		}
		c.Count++
		newer := row.PlayedAt.After(c.LastPlayed) ||
			(row.PlayedAt.Equal(c.LastPlayed) && row.ID > c.ID)
		if newer {
			c.LastPlayed = row.PlayedAt
			c.ID = row.ID
			if row.Title != "" {
				c.Title = row.Title
			}
		}
	}

	out := make([]PlayCount, 0, len(byURL))
	for _, c := range byURL {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].LastPlayed.After(out[j].LastPlayed)
	})
	return out
}
