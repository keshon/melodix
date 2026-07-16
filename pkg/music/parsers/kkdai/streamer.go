package kkdai

import (
	"sync/atomic"

	"github.com/keshon/melodix/pkg/music/opus"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/rs/zerolog"
)

// Mode selects how kkdai feeds ffmpeg: Link hands ffmpeg the CDN URL; Pipe
// streams the audio through ffmpeg's stdin.
type Mode int

const (
	ModeLink Mode = iota
	ModePipe
)

// Streamer extracts YouTube audio via the kkdai/youtube library (InnerTube +
// signature deciphering); the fallback when ytnative can't produce a plain URL.
type Streamer struct{ Mode Mode }

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

func (s *Streamer) Open(track *parsers.Track, seekSec float64) (opus.Reader, func(), error) {
	if s.Mode == ModePipe {
		return kkdaiPipe(track, seekSec)
	}
	return kkdaiLink(track, seekSec)
}
