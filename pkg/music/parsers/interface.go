package parsers

import "io"

// Streamer opens a track as a PCM byte stream (s16le, 48 kHz, stereo). The
// returned func() is cleanup (kill external processes, close streams). Which
// method the registry calls is decided by the registry entry, not the streamer;
// implementations without a pipe path return an error from PipeStream.
type Streamer interface {
	LinkStream(track *Track, seekSec float64) (io.ReadCloser, func(), error)
	PipeStream(track *Track, seekSec float64) (io.ReadCloser, func(), error)
}
