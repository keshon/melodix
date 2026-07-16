package ytnative

import (
	"net/http"
	"os"
	"testing"

	gopus "github.com/godeps/opus"
	"github.com/keshon/melodix/pkg/music/opus"
)

// TestLivePassthrough exercises the full passthrough path against real YouTube:
// InnerTube → pick Opus/WebM → HTTP stream → demux → framing guard → decode.
// It proves a track plays with no ffmpeg. Opt-in via MELODIX_LIVE_TESTS=1.
func TestLivePassthrough(t *testing.T) {
	if os.Getenv("MELODIX_LIVE_TESTS") == "" {
		t.Skip("set MELODIX_LIVE_TESTS=1 to hit real YouTube")
	}
	pr, err := fetchPlayer(httpClient, playerEndpoint, "dQw4w9WgXcQ")
	if err != nil {
		t.Fatalf("fetchPlayer: %v", err)
	}
	f, ok := pickOpusFormat(pr.StreamingData.AdaptiveFormats)
	if !ok {
		t.Fatal("no opus/webm format offered")
	}
	t.Logf("opus format: %s @ %d bps", f.MimeType, f.Bitrate)

	r, cleanup, err := openPassthrough(f.URL, 0)
	if err != nil {
		t.Fatalf("openPassthrough: %v", err)
	}
	defer cleanup()

	dec, err := gopus.NewDecoder(opus.SampleRate, opus.Channels)
	if err != nil {
		t.Fatal(err)
	}
	pcm := make([]int16, opus.FrameSize*opus.Channels)
	n, bad := 0, 0
	for ; n < 100; n++ {
		pkt, err := r.ReadPacket()
		if err != nil {
			break
		}
		if !opus.IsSingle20ms(pkt) {
			bad++
		}
		if _, err := dec.Decode(pkt, pcm); err != nil {
			t.Fatalf("decode packet %d: %v", n, err)
		}
	}
	t.Logf("passthrough: read %d packets, %d not single-20ms, all decoded ✓", n, bad)
	if n < 50 || bad != 0 {
		t.Fatalf("passthrough not clean: %d packets, %d bad", n, bad)
	}
}

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
