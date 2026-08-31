package youtube

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// Live canaries for playlist expansion. InnerTube response shapes drift and the
// two endpoints drift independently, so each kind of list gets its own check.
// Opt-in: MELODIX_LIVE_TESTS=1 go test -run Live -v ./pkg/music/sources/youtube

func liveFetcher(t *testing.T) *PlaylistFetcher {
	t.Helper()
	if os.Getenv("MELODIX_LIVE_TESTS") == "" {
		t.Skip("set MELODIX_LIVE_TESTS=1 to hit real YouTube")
	}
	return NewPlaylistFetcher()
}

// TestLivePlaylistBrowse is the canary for the /browse path: the renderer
// shape, the legacy continuation token, and the shared client version.
func TestLivePlaylistBrowse(t *testing.T) {
	f := liveFetcher(t)

	// A stable public playlist with more entries than one page holds, so this
	// also proves continuation still works.
	got, err := f.Fetch("PLF3eNE6vR-4WsBf8qnJLqBywqX39QczxJ", "")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(got.Entries) <= playlistPageSize {
		t.Fatalf("got %d entries, expected more than one page (%d) — continuation may have broken",
			len(got.Entries), playlistPageSize)
	}
	if got.Title == "" {
		t.Error("playlist title is empty; playlistHeaderRenderer may have moved")
	}
	for i, e := range got.Entries {
		if e.VideoID == "" {
			t.Fatalf("entry %d has no video id", i)
		}
		// Titles are the whole reason to expand server-side rather than queue
		// bare URLs, so an all-empty result is a regression even if ids parse.
		if strings.TrimSpace(e.Title) != "" {
			return
		}
	}
	t.Error("no entry carried a title")
}

// TestLivePlaylistMix is the canary for the /next path, which uses the WEB
// client rather than the shared one; see fetchMix.
func TestLivePlaylistMix(t *testing.T) {
	f := liveFetcher(t)

	got, err := f.Fetch("RDdQw4w9WgXcQ", "dQw4w9WgXcQ")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(got.Entries) < 2 {
		t.Fatalf("mix returned %d entries", len(got.Entries))
	}
	if got.Entries[0].VideoID != "dQw4w9WgXcQ" {
		t.Errorf("seed video should lead the mix, got %q", got.Entries[0].VideoID)
	}
	t.Logf("mix %q: %d entries", got.Title, len(got.Entries))
}

// TestLivePlaylistUnavailable pins the failure shape. It is worth its own
// canary because the shape is not obvious: an unknown list id comes back as an
// HTTP 4xx, while the 200-with-ERROR-alert form is reserved for lists YouTube
// can see but refuses to serve. Both must arrive as ErrPlaylistUnavailable.
func TestLivePlaylistUnavailable(t *testing.T) {
	f := liveFetcher(t)

	// Well-formed but nonexistent, which is what a deleted or private playlist
	// looks like from here: YouTube answers 404, not an ERROR alert.
	_, err := f.Fetch("PLF3eNE6vR-4WsBf8qnJLqBywqX39QczxZ", "")
	// Specifically this sentinel: "any error" would also be satisfied by the
	// network being down, which would make this canary green while blind.
	if !errors.Is(err, ErrPlaylistUnavailable) {
		t.Fatalf("err = %v, want ErrPlaylistUnavailable", err)
	}
	t.Logf("unavailable playlist reported as: %v", err)
}

// TestLiveSearch is the canary for the /search path. It shares the client
// version with playback, so a failure here usually means the same bump.
func TestLiveSearch(t *testing.T) {
	if os.Getenv("MELODIX_LIVE_TESTS") == "" {
		t.Skip("set MELODIX_LIVE_TESTS=1 to hit real YouTube")
	}

	got, err := NewSearcher().Search("daft punk around the world", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("results = %d, want 5", len(got))
	}
	for i, r := range got {
		if r.ID == "" || strings.TrimSpace(r.Title) == "" {
			t.Fatalf("result %d is unusable for a chooser: %+v", i, r)
		}
		t.Logf("%d. %s | %s | %s | %v", i+1, r.ID, r.Title, r.Author, r.Duration)
	}
}
