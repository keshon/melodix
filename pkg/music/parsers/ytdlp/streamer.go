package ytdlp

import (
	"io"

	"github.com/keshon/melodix/pkg/music/parsers"
)

// YtdlpPath is the yt-dlp binary invoked by this parser; override for non-PATH installs.
var YtdlpPath = "yt-dlp"

type Streamer struct{}

func (s *Streamer) LinkStream(track *parsers.TrackParse, seekSec float64) (io.ReadCloser, func(), error) {
	return ytdlpLink(track, seekSec)
}
func (s *Streamer) PipeStream(track *parsers.TrackParse, seekSec float64) (io.ReadCloser, func(), error) {
	return ytdlpPipe(track, seekSec)
}
func (s *Streamer) SupportsPipe() bool {
	return true
}
