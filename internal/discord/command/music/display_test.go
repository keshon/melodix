package music

import (
	"strings"
	"testing"
	"time"

	"github.com/keshon/melodix/internal/domain"
)

func TestFormatTimelineLineShape(t *testing.T) {
	t.Parallel()
	m := domain.MusicPlayback{
		ID:        7,
		PlayedAt:  time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC),
		URL:       "https://x.test/a",
		Title:     "Hi",
	}
	s := FormatTimelineLine(m)
	if !strings.Contains(s, "`7`") || !strings.Contains(s, "[Hi]") || !strings.Contains(s, "`15 Mar 2026`") {
		t.Fatalf("got %q", s)
	}
}

func TestFormatCountsLineNoDate(t *testing.T) {
	t.Parallel()
	r := domain.PlaybackCountRow{
		RepresentativeID: 9,
		URL:              "https://y.test/b",
		Title:            "Song",
		Count:            4,
		LastPlayed:       time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	s := FormatCountsLine(r)
	if strings.Contains(s, "2020") || strings.Contains(s, "Jan") {
		t.Fatalf("counts line should not include date: %q", s)
	}
	if !strings.HasSuffix(s, "`×4`") || !strings.Contains(s, "`9`") || strings.Contains(s, "last ") {
		t.Fatalf("got %q", s)
	}
}
