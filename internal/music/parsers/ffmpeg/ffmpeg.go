package ffmpeg

import (
	"errors"
	"io"

	"github.com/keshon/melodix/internal/music/parsers"
)

const (
	channels   = 2
	sampleRate = 48000
	frameSize  = 960 // 20ms at 48kHz
)

type FFMPEGStreamer struct{}

func (s *FFMPEGStreamer) GetLinkStream(track *parsers.TrackParse, seekSec float64) (io.ReadCloser, func(), error) {
	return ffmpegLink(track.URL)
}
func (s *FFMPEGStreamer) GetPipeStream(track *parsers.TrackParse, seekSec float64) (io.ReadCloser, func(), error) {
	return nil, nil, errors.New("pipe streaming not supported for now")
}
func (s *FFMPEGStreamer) SupportsPipe() bool {
	return false
}
