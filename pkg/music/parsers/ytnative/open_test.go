package ytnative

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/keshon/melodix/pkg/music/opus"
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

// The fixtures below drive ytnativeLink all the way to Opus packets over a stub
// CDN, so the fetch path openCDNBody chooses is exercised through the function
// the engine actually calls rather than only through its helpers — the gap this
// file exists for. The canonical WebM muxer is opus/opus_test.go's muxWebM;
// this is a trimmed copy, since that one is unexported in its own package.

// webmSize encodes an EBML element size (one- or two-byte form, which covers
// everything these fixtures need).
func webmSize(n int) []byte {
	if n < 0x7F {
		return []byte{byte(0x80 | n)}
	}
	return []byte{0x40 | byte(n>>8), byte(n)}
}

func webmElem(id, payload []byte) []byte {
	return append(append(append([]byte{}, id...), webmSize(len(payload))...), payload...)
}

// opusFrame builds a packet whose TOC says "one 20ms frame" — the only framing
// opus.Passthrough will forward. The payload is filler: nothing on this path
// decodes it.
func opusFrame(n int, fill byte) []byte {
	pkt := make([]byte, n)
	pkt[0] = 0x08 // config 1 (20ms), code 0 (single frame)
	for i := 1; i < n; i++ {
		pkt[i] = fill
	}
	return pkt
}

// muxOpusWebM wraps packets in a minimal WebM stream shaped like YouTube's:
// unknown-size Segment and Cluster, one Opus audio track numbered 1.
func muxOpusWebM(pkts [][]byte) []byte {
	unknownSize := []byte{0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	trackEntry := webmElem([]byte{0xAE}, concat(
		webmElem([]byte{0xD7}, []byte{0x01}),     // TrackNumber = 1
		webmElem([]byte{0x83}, []byte{0x02}),     // TrackType = audio
		webmElem([]byte{0x86}, []byte("A_OPUS")), // CodecID
	))
	tracks := webmElem([]byte{0x16, 0x54, 0xAE, 0x6B}, trackEntry)

	var blocks []byte
	for _, pkt := range pkts {
		body := concat([]byte{0x81, 0x00, 0x00, 0x00}, pkt) // track 1, timecode, flags
		blocks = append(blocks, concat([]byte{0xA3}, webmSize(len(body)), body)...)
	}
	cluster := concat([]byte{0x1F, 0x43, 0xB6, 0x75}, unknownSize, blocks)
	segment := concat([]byte{0x18, 0x53, 0x80, 0x67}, unknownSize, tracks, cluster)
	return concat(webmElem([]byte{0x1A, 0x45, 0xDF, 0xA3}, nil), segment)
}

func concat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// mediaFixture returns a WebM stream and the packets muxed into it, sized so it
// spans several chunks at the test chunk size.
func mediaFixture(n int) ([]byte, [][]byte) {
	pkts := make([][]byte, n)
	for i := range pkts {
		pkts[i] = opusFrame(400, byte(i%251))
	}
	return muxOpusWebM(pkts), pkts
}

// playerResponseFor is vodPlayerResponse with the Opus format pointed at a stub
// CDN, so ytnativeLink fetches real bytes.
func playerResponseFor(cdnURL string) string {
	return fmt.Sprintf(`{
		"playabilityStatus": {"status": "OK"},
		"videoDetails": {"title": "A Song", "lengthSeconds": "213"},
		"streamingData": {"adaptiveFormats": [
			{"url": %q, "mimeType": "audio/webm; codecs=\"opus\"", "bitrate": 136544}
		]}
	}`, cdnURL)
}

// readAllPackets drains a parser's reader, guarding against a fixture that
// somehow never ends.
func readAllPackets(t *testing.T, r opus.Reader) [][]byte {
	t.Helper()
	var got [][]byte
	for len(got) < 10000 {
		pkt, err := r.ReadPacket()
		if errors.Is(err, io.EOF) {
			return got
		}
		if err != nil {
			t.Fatalf("ReadPacket after %d packets: %v", len(got), err)
		}
		got = append(got, pkt)
	}
	t.Fatal("reader never reached EOF")
	return nil
}

func TestOpenPlaysAVODThroughTheChunkedFetcher(t *testing.T) {
	withChunkSize(t, 4<<10)
	media, want := mediaFixture(40)
	cdn, log := rangedServer(t, media, nil)
	withPlayerResponse(t, playerResponseFor(cdn.URL))

	track := testTrack()
	r, cleanup, err := ytnativeLink(track, 0)
	if err != nil {
		t.Fatalf("ytnativeLink: %v", err)
	}
	defer cleanup()

	got := readAllPackets(t, r)
	if len(got) != len(want) {
		t.Fatalf("played %d packets, want %d", len(got), len(want))
	}
	for i := range got {
		if !bytes.Equal(got[i], want[i]) {
			t.Fatalf("packet %d differs across the chunk boundaries", i)
		}
	}
	if !track.Passthrough {
		t.Error("track not marked passthrough")
	}
	// More than one range means it really went through the chunked fetcher and
	// not the single-response fallback.
	if n := len(log.snapshot()); n < 2 {
		t.Fatalf("made %d range requests, want the file fetched in chunks", n)
	}
}

func TestOpenPlaysASourceThatWillNotServeRanges(t *testing.T) {
	withChunkSize(t, 4<<10)
	media, want := mediaFixture(40)
	// A source that answers every request with the whole body and never states a
	// length — an open-ended stream, which must still play.
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(media)
	}))
	defer cdn.Close()
	withPlayerResponse(t, playerResponseFor(cdn.URL))

	track := testTrack()
	r, cleanup, err := ytnativeLink(track, 0)
	if err != nil {
		t.Fatalf("ytnativeLink: %v", err)
	}
	defer cleanup()

	got := readAllPackets(t, r)
	if len(got) != len(want) {
		t.Fatalf("played %d packets over the streaming fallback, want %d", len(got), len(want))
	}
	if !track.Passthrough {
		t.Error("track not marked passthrough")
	}
}

func TestOpenSeeksByDiscardingPackets(t *testing.T) {
	withChunkSize(t, 4<<10)
	media, want := mediaFixture(40)
	cdn, _ := rangedServer(t, media, nil)
	withPlayerResponse(t, playerResponseFor(cdn.URL))

	// 0.4s in: twenty 20ms packets discarded, the rest played.
	r, cleanup, err := ytnativeLink(testTrack(), 0.4)
	if err != nil {
		t.Fatalf("ytnativeLink: %v", err)
	}
	defer cleanup()

	got := readAllPackets(t, r)
	if len(got) != len(want)-20 {
		t.Fatalf("played %d packets after a 0.4s seek, want %d", len(got), len(want)-20)
	}
	if !bytes.Equal(got[0], want[20]) {
		t.Fatal("seek did not land on the packet at 0.4s")
	}
}
