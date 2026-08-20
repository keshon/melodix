package common

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/keshon/melodix/pkg/music/parsers"
)

func TestFormatQueueLineShape(t *testing.T) {
	t.Parallel()
	s := FormatQueueLine(3, "Song", "https://x.test/a", 225*time.Second)
	if !strings.HasPrefix(s, "`3` ") || !strings.Contains(s, "[Song](https://x.test/a)") || !strings.HasSuffix(s, "`3:45`") {
		t.Fatalf("got %q", s)
	}
}

func TestFormatQueueLineOmitsUnknownDuration(t *testing.T) {
	t.Parallel()
	s := FormatQueueLine(1, "Song", "https://x.test/a", 0)
	if strings.Contains(s, "0:00") {
		t.Fatalf("zero duration should render no chip: %q", s)
	}
}

func TestTrackLabelFallsBackToURL(t *testing.T) {
	t.Parallel()
	// Queued-but-unopened tracks usually have no title yet.
	s := FormatQueueLine(1, "", "https://x.test/a", 0)
	if !strings.Contains(s, "[https://x.test/a](https://x.test/a)") {
		t.Fatalf("got %q", s)
	}
	if got := trackLabel("", "", 0); got != "(no title)" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatQueueLineHoursDuration(t *testing.T) {
	t.Parallel()
	s := FormatQueueLine(1, "Long", "https://x.test/a", 3602*time.Second)
	if !strings.HasSuffix(s, "`1:00:02`") {
		t.Fatalf("got %q", s)
	}
}

func TestFormatQueueBodyEmpty(t *testing.T) {
	t.Parallel()
	if got := FormatQueueBody(nil, nil); !strings.Contains(got, "queue is empty") {
		t.Fatalf("got %q", got)
	}
}

func TestFormatQueueBodyCurrentOnly(t *testing.T) {
	t.Parallel()
	cur := &parsers.Track{Title: "Now", URL: "https://x.test/now"}
	got := FormatQueueBody(cur, nil)
	if !strings.HasPrefix(got, "▶️ [Now](https://x.test/now)") {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(got, "Nothing queued after this.") {
		t.Fatalf("got %q", got)
	}
}

func TestFormatQueueBodyNumbersFromOne(t *testing.T) {
	t.Parallel()
	up := []parsers.Track{
		{Title: "A", URL: "https://x.test/a"},
		{Title: "B", URL: "https://x.test/b"},
	}
	got := FormatQueueBody(nil, up)
	if !strings.Contains(got, "`1` [A]") || !strings.Contains(got, "`2` [B]") {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, "more") {
		t.Fatalf("no remainder expected: %q", got)
	}
}

func TestFormatQueueBodyTruncatesWithRemainder(t *testing.T) {
	t.Parallel()
	up := make([]parsers.Track, queueLinesShown+60)
	for i := range up {
		up[i] = parsers.Track{Title: fmt.Sprintf("T%d", i), URL: "https://x.test/t"}
	}
	got := FormatQueueBody(nil, up)
	if strings.Count(got, "\n`") != queueLinesShown-1 {
		t.Fatalf("expected %d rows, got %q", queueLinesShown, got)
	}
	if !strings.Contains(got, "…and 60 more") {
		t.Fatalf("got %q", got)
	}
}
