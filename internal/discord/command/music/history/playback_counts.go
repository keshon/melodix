package history

import (
	"sort"
	"time"

	"github.com/keshon/melodix/internal/storage"
)

// playbackCountRow is one distinct URL in counts mode (representative id is the
// latest row for that URL, so /play <id> replays the newest entry).
type playbackCountRow struct {
	RepresentativeID uint64
	URL              string
	Title            string
	Count            int
	LastPlayed       time.Time
}

// aggregatePlaybackCounts groups history by URL (string equality).
// Sort: count desc, then last played desc.
func aggregatePlaybackCounts(history []storage.PlaybackEntry) []playbackCountRow {
	type agg struct {
		latestID uint64
		url      string
		title    string
		count    int
		last     time.Time
	}
	byURL := make(map[string]*agg)
	for _, row := range history {
		u := row.URL
		a, ok := byURL[u]
		if !ok {
			byURL[u] = &agg{
				latestID: row.ID,
				url:      row.URL,
				title:    row.Title,
				count:    1,
				last:     row.PlayedAt,
			}
			continue
		}
		a.count++
		if row.PlayedAt.After(a.last) {
			a.last = row.PlayedAt
			a.latestID = row.ID
			if row.Title != "" {
				a.title = row.Title
			}
		} else if row.PlayedAt.Equal(a.last) && row.ID > a.latestID {
			a.latestID = row.ID
			if row.Title != "" {
				a.title = row.Title
			}
		}
	}

	out := make([]playbackCountRow, 0, len(byURL))
	for _, a := range byURL {
		out = append(out, playbackCountRow{
			RepresentativeID: a.latestID,
			URL:              a.url,
			Title:            a.title,
			Count:            a.count,
			LastPlayed:       a.last,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].LastPlayed.After(out[j].LastPlayed)
	})
	return out
}
