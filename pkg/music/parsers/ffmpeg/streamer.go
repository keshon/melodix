package ffmpeg

import (
	"errors"
	"io"

	"github.com/keshon/melodix/pkg/music/parsers"
)

const (
	channels   = 2
	sampleRate = 48000
)

// Streamer plays a URL by handing it directly to ffmpeg (used for radio streams).
type Streamer struct{}

func (s *Streamer) LinkStream(track *parsers.Track, seekSec float64) (io.ReadCloser, func(), error) {
	return ffmpegLink(track.URL)
}
func (s *Streamer) PipeStream(track *parsers.Track, seekSec float64) (io.ReadCloser, func(), error) {
	return nil, nil, errors.New("ffmpeg: pipe streaming not supported")
}
