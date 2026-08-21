package kkdai

import (
	"os"
	"testing"

	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/sources"
)

// kkdai resolves through the real YouTube API, so there is no useful seam short
// of the network — these are opt-in via MELODIX_LIVE_TESTS=1. They exist because
// both kkdai paths once carried a gate on video.HLSManifestURL, which is not a
// live-broadcast signal under the VISIONOS client this parser rides: Apple
// clients attach an HLS manifest to ordinary VOD, so the gate refused every
// playable track and caught no live one. Nothing at the time exercised Open.

const vodURL = "https://www.youtube.com/watch?v=dQw4w9WgXcQ"

func liveTrack(url string) *parsers.Track {
	return &parsers.Track{URL: url, SourceInfo: sources.TrackInfo{URL: url}}
}

func skipUnlessLive(t *testing.T) {
	t.Helper()
	if os.Getenv("MELODIX_LIVE_TESTS") == "" {
		t.Skip("set MELODIX_LIVE_TESTS=1 to hit real YouTube")
	}
}

// The pipe path is the passthrough one; an ordinary video must reach a
// WebM/Opus format rather than being turned away.
func TestLivePipeOpensAnOrdinaryVideo(t *testing.T) {
	skipUnlessLive(t)

	track := liveTrack(vodURL)
	r, cleanup, err := kkdaiPipe(track, 0)
	if err != nil {
		t.Fatalf("kkdai pipe refused an ordinary video: %v", err)
	}
	defer cleanup()

	if _, err := r.ReadPacket(); err != nil {
		t.Fatalf("no packet from a video that opened: %v", err)
	}
	if track.Title == "" {
		t.Error("title not filled in at open time")
	}
	t.Logf("pipe opened %q, duration %s", track.Title, track.Duration)
}

// The link path hands ffmpeg a CDN URL. Same requirement: an ordinary video
// must not be declined before a format is ever chosen.
func TestLiveLinkOpensAnOrdinaryVideo(t *testing.T) {
	skipUnlessLive(t)

	track := liveTrack(vodURL)
	r, cleanup, err := kkdaiLink(track, 0)
	if err != nil {
		t.Fatalf("kkdai link refused an ordinary video: %v", err)
	}
	defer cleanup()

	if _, err := r.ReadPacket(); err != nil {
		t.Fatalf("no packet from a video that opened: %v", err)
	}
	t.Logf("link opened %q, duration %s", track.Title, track.Duration)
}
