package soundcloud

import (
	"os"
	"strings"
	"testing"
)

// Live canary for the search path. api-v2 is an undocumented API behind a
// rotating client_id, so drift here is a question of when.
// Opt-in: MELODIX_LIVE_TESTS=1 go test -run Live -v ./pkg/music/sources/soundcloud

func TestLiveSearchAndPermalinkRoundTrip(t *testing.T) {
	if os.Getenv("MELODIX_LIVE_TESTS") == "" {
		t.Skip("set MELODIX_LIVE_TESTS=1 to hit real SoundCloud")
	}

	s := NewSearcher()
	hits, err := s.Search("lofi", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 5 {
		t.Fatalf("results = %d, want 5", len(hits))
	}
	for i, h := range hits {
		if h.ID == "" || h.URL == "" || strings.TrimSpace(h.Title) == "" {
			t.Fatalf("result %d is unusable for a chooser: %+v", i, h)
		}
		t.Logf("%d. id=%s | %s | %s | %v", i+1, h.ID, h.Title, h.Author, h.Duration)
	}

	// The chooser hands back an id, not a URL, so this hop has to work or a
	// pressed button resolves to nothing.
	url, err := s.PermalinkByID(hits[0].ID)
	if err != nil {
		t.Fatalf("PermalinkByID(%q): %v", hits[0].ID, err)
	}
	if url != hits[0].URL {
		t.Errorf("round trip gave %q, search gave %q", url, hits[0].URL)
	}
}
