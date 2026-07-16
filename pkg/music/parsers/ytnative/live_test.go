package ytnative

import (
	"net/http"
	"os"
	"testing"
)

// TestLiveInnerTube hits the real InnerTube API with the ANDROID_VR client and checks
// that a direct (cipher-free) audio URL comes back and the CDN accepts our UA.
// This is the canary for clientVersion rot.
// Opt-in: MELODIX_LIVE_TESTS=1 go test -run Live -v ./pkg/music/parsers/ytnative
func TestLiveInnerTube(t *testing.T) {
	if os.Getenv("MELODIX_LIVE_TESTS") == "" {
		t.Skip("set MELODIX_LIVE_TESTS=1 to hit real YouTube")
	}

	pr, err := fetchPlayer(httpClient, playerEndpoint, "dQw4w9WgXcQ")
	if err != nil {
		t.Fatalf("fetchPlayer (clientVersion %s may need a bump): %v", clientVersion, err)
	}
	t.Logf("title: %s, length: %ss, formats: %d",
		pr.VideoDetails.Title, pr.VideoDetails.LengthSeconds, len(pr.StreamingData.AdaptiveFormats))

	f, err := pickAudioFormat(pr.StreamingData.AdaptiveFormats)
	if err != nil {
		t.Fatalf("pickAudioFormat: %v", err)
	}
	t.Logf("picked: %s @ %d bps", f.MimeType, f.Bitrate)

	req, err := http.NewRequest(http.MethodGet, f.URL, nil)
	if err != nil {
		t.Fatalf("build CDN request: %v", err)
	}
	req.Header.Set("User-Agent", clientUserAgent)
	req.Header.Set("Range", "bytes=0-1023")
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("fetch stream url: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("CDN returned %s (403 here usually means poToken enforcement)", resp.Status)
	}
	t.Logf("CDN reachable: %s", resp.Status)
}
