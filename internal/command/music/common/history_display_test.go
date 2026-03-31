package common

import (
	"strings"
	"testing"
	"time"

	"github.com/keshon/melodix/internal/domain"
)

func TestTruncateTitleMiddle(t *testing.T) {
	t.Parallel()
	short := "abc"
	if got := truncateTitleMiddle(short, 10); got != short {
		t.Fatalf("short: %q", got)
	}
	long := "abcdefghijklmnopqrstuvwxyz0123456789"
	got := truncateTitleMiddle(long, 12)
	if len([]rune(got)) != 12 {
		t.Fatalf("rune len: %q len=%d", got, len([]rune(got)))
	}
	if !strings.Contains(got, "...") {
		t.Fatalf("expected ellipsis: %q", got)
	}
}

func TestFormatTimelineLineShape(t *testing.T) {
	t.Parallel()
	m := domain.MusicPlayback{
		ID:       7,
		PlayedAt: time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC),
		URL:      "https://x.test/a",
		Title:    "Hi",
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

