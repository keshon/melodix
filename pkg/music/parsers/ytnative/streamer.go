// Package ytnative streams YouTube audio via a thin InnerTube client (ANDROID
// context, direct cipher-free URLs) piped through ffmpeg. No JS engine, no
// signature deciphering — protected videos fail fast into the kkdai/yt-dlp
// fallback parsers.
package ytnative

import (
	"errors"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/rs/zerolog"
)

type Streamer struct{}

var httpClient = &http.Client{Timeout: 10 * time.Second}

func (s *Streamer) LinkStream(track *parsers.Track, seekSec float64) (io.ReadCloser, func(), error) {
	return ytnativeLink(track, seekSec)
}
func (s *Streamer) PipeStream(track *parsers.Track, seekSec float64) (io.ReadCloser, func(), error) {
	return nil, nil, errors.New("ytnative: pipe streaming not supported")
}

var logPtr atomic.Pointer[zerolog.Logger]

// SetLogger sets the package logger (playability/client-version diagnostics).
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
