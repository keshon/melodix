package ytnative

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/sources"
)

// These cover ytnativeLink itself rather than fetchPlayer, which is the gap that
// let a broken live-stream gate ship: every existing test — the unit ones and
// the opt-in live ones both — called fetchPlayer and pickOpusFormat directly,
// so a check sitting between them and the caller was exercised by nothing.

// withPlayerResponse points ytnativeLink at a stub player endpoint returning
// body, and restores the real one afterwards.
func withPlayerResponse(t *testing.T, body string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)

	origEndpoint, origClient := playerEndpoint, httpClient
	playerEndpoint, httpClient = srv.URL, srv.Client()
	t.Cleanup(func() { playerEndpoint, httpClient = origEndpoint, origClient })
}

func testTrack() *parsers.Track {
	return &parsers.Track{
		URL:        "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		SourceInfo: sources.TrackInfo{URL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
	}
}

// vodPlayerResponse is the shape VISIONOS actually returns for an ordinary
// video: playable, a real duration, and — the part that matters — an
// hlsManifestUrl sitting right next to usable Opus formats. Verified against the
// live API on a 213-second music video.
const vodPlayerResponse = `{
	"playabilityStatus": {"status": "OK"},
	"videoDetails": {"title": "A Song", "lengthSeconds": "213", "isLiveContent": false},
	"streamingData": {
		"hlsManifestUrl": "https://manifest.googlevideo.com/api/manifest/hls_playlist/x.m3u8",
		"adaptiveFormats": [
			{"url": "https://cdn/opus", "mimeType": "audio/webm; codecs=\"opus\"", "bitrate": 136544},
			{"url": "https://cdn/m4a", "mimeType": "audio/mp4; codecs=\"mp4a.40.2\"", "bitrate": 129000}
		]
	}
}`

// A VOD carrying hlsManifestUrl must still resolve to a format. Treating that
// field as a live-broadcast signal rejected every playable YouTube video and
// pushed the whole chain onto yt-dlp, adding roughly twenty seconds per track
// and losing Opus passthrough entirely.
func TestOpenAcceptsAVODThatCarriesAnHLSManifest(t *testing.T) {
	withPlayerResponse(t, vodPlayerResponse)

	track := testTrack()
	pr, err := fetchPlayer(httpClient, playerEndpoint, "dQw4w9WgXcQ")
	if err != nil {
		t.Fatalf("fetchPlayer on a playable VOD: %v", err)
	}
	if pr.StreamingData.HLSManifestURL == "" {
		t.Fatal("fixture must carry hlsManifestUrl, otherwise it tests nothing")
	}
	if _, ok := pickOpusFormat(pr.StreamingData.AdaptiveFormats); !ok {
		t.Fatal("no Opus format picked from a VOD that plainly has one")
	}

	// The real assertion: Open gets past metadata and format selection to the
	// point of fetching the CDN URL, rather than bailing out early. The stub
	// serves JSON rather than media, so a transport-shaped failure is expected
	// and fine — a live-stream rejection is not.
	_, _, err = ytnativeLink(track, 0)
	if err != nil && errors.Is(err, ErrNotPlayable) {
		t.Fatalf("VOD rejected as not playable: %v", err)
	}
	if track.Title != "A Song" {
		t.Fatalf("title not filled from the player response: %q", track.Title)
	}
}

// A live broadcast is refused upstream, by playability, not by any gate of our
// own: VISIONOS answers one with UNPLAYABLE and no formats. This locks in that
// the chain still gets a fast, unambiguous failure so it can reach yt-dlp.
func TestOpenRejectsALiveBroadcast(t *testing.T) {
	withPlayerResponse(t, `{
		"playabilityStatus": {"status": "UNPLAYABLE", "reason": "This live event has not started"},
		"videoDetails": {"title": "lofi radio", "lengthSeconds": "121601512", "isLiveContent": true},
		"streamingData": {"adaptiveFormats": []}
	}`)

	_, _, err := ytnativeLink(testTrack(), 0)
	if err == nil {
		t.Fatal("a live broadcast opened successfully")
	}
	if !errors.Is(err, ErrNotPlayable) {
		t.Fatalf("want ErrNotPlayable so the chain moves on, got %v", err)
	}
}

// The live fixture's giant lengthSeconds is not a typo — a 24/7 broadcast
// reports one, so "duration is zero" is not a live signal either. Guards this
// against a future fix that reaches for duration instead.
func TestLiveBroadcastReportsANonZeroDuration(t *testing.T) {
	withPlayerResponse(t, `{
		"playabilityStatus": {"status": "OK"},
		"videoDetails": {"title": "lofi radio", "lengthSeconds": "121601512", "isLiveContent": true},
		"streamingData": {"adaptiveFormats": []}
	}`)

	pr, err := fetchPlayer(httpClient, playerEndpoint, "jfKfPfyJRdk")
	if err != nil {
		t.Fatalf("fetchPlayer: %v", err)
	}
	if pr.VideoDetails.LengthSeconds == "0" || pr.VideoDetails.LengthSeconds == "" {
		t.Fatal("fixture no longer represents a live broadcast's duration")
	}
}
