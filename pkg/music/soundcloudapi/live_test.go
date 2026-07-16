package soundcloudapi

import (
	"net/http"
	"os"
	"testing"
)

// TestLiveSoundCloudPipeline hits the real SoundCloud endpoints: client_id scrape →
// search → resolve → transcoding pick → signed stream URL → HTTP reachability.
// Opt-in (network + third-party dependent): MELODIX_LIVE_TESTS=1 go test -run Live -v ./pkg/music/soundcloudapi
func TestLiveSoundCloudPipeline(t *testing.T) {
	if os.Getenv("MELODIX_LIVE_TESTS") == "" {
		t.Skip("set MELODIX_LIVE_TESTS=1 to hit real SoundCloud")
	}

	c := New()
	id, err := c.ClientID()
	if err != nil {
		t.Fatalf("client_id scrape: %v", err)
	}
	t.Logf("client_id: %s...", id[:min(6, len(id))])

	track, err := c.SearchFirstTrack("daft punk")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	t.Logf("search hit: %s (%s)", track.Title, track.PermalinkURL)

	resolved, err := c.ResolveTrack(track.PermalinkURL)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	tr, err := PickTranscoding(resolved.Media.Transcodings)
	if err != nil {
		t.Fatalf("pick transcoding: %v", err)
	}
	t.Logf("transcoding: protocol=%s preset=%s", tr.Format.Protocol, tr.Preset)

	streamURL, err := c.StreamURL(tr)
	if err != nil {
		t.Fatalf("stream url: %v", err)
	}
	resp, err := c.HTTP.Get(streamURL)
	if err != nil {
		t.Fatalf("fetch stream url: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream url returned %s", resp.Status)
	}
	t.Logf("stream url reachable: %s, content-type %s", resp.Status, resp.Header.Get("Content-Type"))
}
