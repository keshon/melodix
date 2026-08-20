// Package ytnative streams YouTube audio via a thin InnerTube client (VISIONOS
// context, direct cipher-free URLs), preferring Opus passthrough and falling
// back to ffmpeg. No JS engine, no signature deciphering, no PO token —
// protected videos fail fast into the kkdai/yt-dlp fallbacks.
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

// maxBitrate caps which audio format is chosen, in bits per second (0 = no cap,
// take the best on offer). It exists for links that cannot afford the best:
// YouTube offers the same track at roughly 49, 66 and 137 kbps, so capping turns
// a 3.3 MiB download into a 1.2 MiB one — which is both less to stream and, when
// a stream has to be reopened after a drop, far less to re-fetch. A Discord
// voice channel is 64 kbps unless the guild is boosted, so the top format is
// largely spent on bandwidth that the channel will not carry anyway.
var maxBitrate atomic.Int64

// SetMaxBitrate sets the audio-format ceiling in bits per second (<=0 disables
// the cap). Safe for concurrent use; call once at process startup.
func SetMaxBitrate(bps int) {
	if bps < 0 {
		bps = 0
	}
	maxBitrate.Store(int64(bps))
}

// withinBitrateCap filters formats to those at or under the cap. If the cap
// excludes everything it is ignored rather than obeyed: a quieter track is the
// goal, silence is not.
func withinBitrateCap(formats []format) []format {
	limit := maxBitrate.Load()
	if limit <= 0 {
		return formats
	}
	kept := make([]format, 0, len(formats))
	for _, f := range formats {
		if int64(f.Bitrate) <= limit {
			kept = append(kept, f)
		}
	}
	if len(kept) == 0 {
		return formats
	}
	return kept
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
