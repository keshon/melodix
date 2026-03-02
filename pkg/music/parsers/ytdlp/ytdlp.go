package ytdlp

import (
	"io"

	"github.com/keshon/melodix/pkg/music/parsers"
)

const (
	channels   = 2
	sampleRate = 48000
	frameSize  = 960 // 20ms at 48kHz
)

type YTDLPStreamer struct{}

func (s *YTDLPStreamer) GetLinkStream(track *parsers.TrackParse, seekSec float64) (io.ReadCloser, func(), error) {
	return ytdlpLink(track, seekSec)
}
func (s *YTDLPStreamer) GetPipeStream(track *parsers.TrackParse, seekSec float64) (io.ReadCloser, func(), error) {
	return ytdlpPipe(track, seekSec)
}
func (s *YTDLPStreamer) SupportsPipe() bool {
	return true
}
