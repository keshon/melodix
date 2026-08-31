package kkdai

import (
	"sync/atomic"

	"github.com/keshon/melodix/pkg/music/opus"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/kkdai/youtube/v2"
	"github.com/rs/zerolog"
)

// Mode selects how kkdai feeds ffmpeg: Link hands ffmpeg the CDN URL; Pipe
// streams the audio through ffmpeg's stdin.
type Mode int

const (
	ModeLink Mode = iota
	ModePipe
)

// VisionOSClient mirrors yt-dlp's visionos InnerTube client. kkdai defaults to
// ANDROID_VR, and googlevideo serves an ANDROID_VR stream URL only for bounded
// range requests (roughly 1 MiB at a time), answering 403 to any open-ended one
// — which is precisely what ffmpeg (Range: bytes=0-) and kkdai's own reader ask
// for. VISIONOS URLs serve open-ended requests, so this is what makes both
// kkdai parsers work at all rather than 403 on their first read.
//
// ClientInfo has no deviceMake/osName/osVersion fields, so kkdai sends a
// reduced context; that reduced form is accepted and yields a working URL
// (verified against the live API, not assumed). kkdai documents DefaultClient
// as the knob for this, and the unexported per-Client field leaves no narrower
// option.
var VisionOSClient = youtube.ClientInfo{
	Name:        "VISIONOS",
	Version:     "1.02",
	DeviceModel: "RealityDevice17,1",
	UserAgent:   "Mozilla/5.0 (Macintosh; Intel Mac OS X 15_7_3) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15",
}

func init() { youtube.DefaultClient = VisionOSClient }

// Live broadcasts are not gated here. Both paths ride the VISIONOS client (see
// VisionOSClient), which answers a live video with no formats at all, so each
// path already fails on its own: the pipe path finds no WebM/Opus format and
// the link path finds no audio format. A gate on video.HLSManifestURL was tried
// and was exactly backwards — Apple-platform clients attach an HLS manifest to
// ordinary VOD, so it declined every playable track and no live one.

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
