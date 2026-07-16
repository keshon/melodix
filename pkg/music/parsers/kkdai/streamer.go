package kkdai

import (
	"io"
	"sync/atomic"

	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/rs/zerolog"
)

// Streamer extracts YouTube audio via the kkdai/youtube library (InnerTube +
// signature deciphering); the fallback when ytnative can't produce a plain URL.
type Streamer struct{}

var logPtr atomic.Pointer[zerolog.Logger]

// SetLogger sets an optional logger for kkdai parser internals (debug signals).
// Safe for concurrent use; call once at process startup.
func SetLogger(l zerolog.Logger) {
	logPtr.Store(&l)
}

func logger() zerolog.Logger {
	if l := logPtr.Load(); l != nil {
		return *l
	}
	return zerolog.Nop()
}

func (s *Streamer) LinkStream(track *parsers.Track, seekSec float64) (io.ReadCloser, func(), error) {
	return kkdaiLink(track, seekSec)
}
func (s *Streamer) PipeStream(track *parsers.Track, seekSec float64) (io.ReadCloser, func(), error) {
	return kkdaiPipe(track, seekSec)
}
