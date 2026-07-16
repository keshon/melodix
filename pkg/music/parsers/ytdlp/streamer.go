package ytdlp

import (
	"io"

	"github.com/keshon/melodix/pkg/music/parsers"
)

// YtdlpPath is the yt-dlp binary invoked by this parser; override for non-PATH installs.
var YtdlpPath = "yt-dlp"

// Streamer extracts audio by shelling out to yt-dlp; the fallback of last resort.
type Streamer struct{}

func (s *Streamer) LinkStream(track *parsers.Track, seekSec float64) (io.ReadCloser, func(), error) {
	return ytdlpLink(track, seekSec)
}
func (s *Streamer) PipeStream(track *parsers.Track, seekSec float64) (io.ReadCloser, func(), error) {
	return ytdlpPipe(track, seekSec)
}
