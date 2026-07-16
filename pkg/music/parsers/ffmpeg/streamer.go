package ffmpeg

import (
	"github.com/keshon/melodix/pkg/music/opus"
	"github.com/keshon/melodix/pkg/music/parsers"
)

const (
	channels   = 2
	sampleRate = 48000
)

// Streamer plays a URL by handing it directly to ffmpeg (used for radio streams).
type Streamer struct{}

// Open ignores seekSec — radio streams are live.
func (s *Streamer) Open(track *parsers.Track, seekSec float64) (opus.Reader, func(), error) {
	return ffmpegLink(track.URL)
}
