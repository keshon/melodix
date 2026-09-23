package history

import (
	"strings"
	"testing"
	"time"

	"github.com/keshon/melodix/internal/storage"
)

// Page 1 is what was just played. Storage returns rows oldest first, and the
// timeline used to page them in that order, so a server with a few hundred
// plays opened /history on tracks from weeks ago -- under a command described
// as "Show recently played tracks".
func TestTimelineListsNewestFirst(t *testing.T) {
	t.Parallel()
	day := func(d int) time.Time { return time.Date(2026, 9, d, 0, 0, 0, 0, time.UTC) }
	rows := []storage.PlaybackEntry{
		{ID: 1, PlayedAt: day(1), URL: "https://a.com", Title: "oldest"},
		{ID: 2, PlayedAt: day(2), URL: "https://b.com", Title: "middle"},
		{ID: 3, PlayedAt: day(3), URL: "https://c.com", Title: "newest"},
	}

	lines := timelineLines(rows)

	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	for i, want := range []string{"newest", "middle", "oldest"} {
		if !strings.Contains(lines[i], want) {
			t.Errorf("line %d = %q, want the %s track", i, lines[i], want)
		}
	}
}
