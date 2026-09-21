package parsers

import (
	"time"

	"github.com/keshon/melodix/pkg/music/opus"
)

// Streamer opens a track as a stream of 20ms Opus packets (opus.Reader). How it
// produces them is internal: passthrough sources demux a native Opus container,
// transcode sources run ffmpeg → PCM → encode.
//
// The track is a copy. Whatever the parser learns while opening it comes back
// in Opened, never written into a Track the caller holds.
type Streamer interface {
	Open(track Track, seekSec float64) (Opened, error)
}

// Opened is an open stream and what the parser learned on the way.
type Opened struct {
	Reader opus.Reader
	// Cleanup kills external processes and closes streams.
	Cleanup func()
	// Passthrough reports that native Opus packets are forwarded with no
	// transcode.
	Passthrough bool
	// Title, Artist and Duration are the media's own view of the track, often
	// better than the resolver's. Empty or zero means the parser learned
	// nothing, not that the track has no title or no length.
	Title    string
	Artist   string
	Duration time.Duration
}
