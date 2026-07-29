package common

import (
	"strings"
	"testing"
	"time"
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
	s := FormatTimelineLine(7, "Hi", "https://x.test/a", time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC))
	if !strings.Contains(s, "`7`") || !strings.Contains(s, "[Hi]") || !strings.Contains(s, "`15 Mar 2026`") {
		t.Fatalf("got %q", s)
	}
}

func TestFormatCountsLineNoDate(t *testing.T) {
	t.Parallel()
	s := FormatCountsLine(9, "Song", "https://y.test/b", 4)
	if strings.Contains(s, "2020") || strings.Contains(s, "Jan") {
		t.Fatalf("counts line should not include date: %q", s)
	}
	if !strings.HasSuffix(s, "`×4`") || !strings.Contains(s, "`9`") || strings.Contains(s, "last ") {
		t.Fatalf("got %q", s)
	}
}
