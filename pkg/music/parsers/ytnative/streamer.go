// Package ytnative streams YouTube audio via a thin InnerTube client (ANDROID_VR
// context, direct cipher-free URLs) through ffmpeg. No JS engine, no signature
// deciphering — protected videos fail fast into the kkdai/yt-dlp fallbacks.
package ytnative

import (
	"net/http"
	"sync/atomic"
	"time"

	"github.com/keshon/melodix/pkg/music/opus"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/rs/zerolog"
)

type Streamer struct{}

// httpClient is for the quick InnerTube POST. streamClient has no total timeout
// because a passthrough body streams for the whole track; a dropped connection
// surfaces as a read error and the player's recovery re-opens.
var (
	httpClient   = &http.Client{Timeout: 10 * time.Second}
	streamClient = &http.Client{}
)

func (s *Streamer) Open(track *parsers.Track, seekSec float64) (opus.Reader, func(), error) {
	return ytnativeLink(track, seekSec)
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
