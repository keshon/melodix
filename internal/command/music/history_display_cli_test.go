package music

import (
	"strings"
	"testing"
	"time"

	"github.com/keshon/melodix/internal/domain"
)

func TestFormatTimelineLinePlainShape(t *testing.T) {
	t.Parallel()
	m := domain.MusicPlayback{
		ID:        7,
		PlayedAt:  time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC),
		URL:       "https://x.test/a",
		Title:     "Hi",
	}
	s := formatTimelineLinePlain(m)
	if !strings.Contains(s, "7") || !strings.Contains(s, "Hi") || !strings.Contains(s, "https://x.test/a") {
		t.Fatalf("got %q", s)
	}
	if strings.Contains(s, "[") || strings.Contains(s, "](") {
		t.Fatalf("unexpected markdown: %q", s)
	}
}

func TestFormatCountsLinePlainShape(t *testing.T) {
	t.Parallel()
	r := domain.PlaybackCountRow{
		RepresentativeID: 9,
		URL:              "https://y.test/b",
		Title:            "Song",
		Count:            4,
		LastPlayed:       time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	s := formatCountsLinePlain(r)
	if !strings.Contains(s, "9") || !strings.Contains(s, "x4") {
		t.Fatalf("got %q", s)
	}
}
