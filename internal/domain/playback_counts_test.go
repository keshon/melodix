package domain

import (
	"testing"
	"time"
)

func TestAggregatePlaybackCounts(t *testing.T) {
	t.Parallel()
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	h := []MusicPlayback{
		{ID: 1, PlayedAt: t1, URL: "https://a.com", Title: "A"},
		{ID: 2, PlayedAt: t2, URL: "https://a.com", Title: "A newer"},
		{ID: 3, PlayedAt: t1, URL: "https://b.com", Title: "B"},
	}
	rows := AggregatePlaybackCounts(h)
	if len(rows) != 2 {
		t.Fatalf("want 2 groups, got %d", len(rows))
	}
	// Sorted by count desc: a.com has 2, b.com has 1
	if rows[0].URL != "https://a.com" || rows[0].Count != 2 || rows[0].RepresentativeID != 2 {
		t.Fatalf("first row: %+v", rows[0])
	}
	if rows[1].Count != 1 || rows[1].RepresentativeID != 3 {
		t.Fatalf("second row: %+v", rows[1])
	}
}
