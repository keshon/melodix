package kkdai

import (
	"io"
	"sync/atomic"

	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/rs/zerolog"
)

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

func (s *Streamer) LinkStream(track *parsers.TrackParse, seekSec float64) (io.ReadCloser, func(), error) {
	return kkdaiLink(track, seekSec)
}
func (s *Streamer) PipeStream(track *parsers.TrackParse, seekSec float64) (io.ReadCloser, func(), error) {
	return kkdaiPipe(track, seekSec)
}
func (s *Streamer) SupportsPipe() bool {
	return true
}
