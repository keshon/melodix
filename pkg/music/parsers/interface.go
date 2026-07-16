package parsers

import "github.com/keshon/melodix/pkg/music/opus"

// Streamer opens a track as a stream of 20ms Opus packets (opus.Reader). How it
// produces them is internal: passthrough sources demux a native Opus container,
// transcode sources run ffmpeg → PCM → encode. The returned func() is cleanup
// (kill external processes, close streams).
type Streamer interface {
	Open(track *Track, seekSec float64) (opus.Reader, func(), error)
}
