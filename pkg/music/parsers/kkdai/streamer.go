package kkdai

import (
	"io"

	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/rs/zerolog"
)

const (
	channels   = 2
	sampleRate = 48000
)

type Streamer struct{}

var log = zerolog.Nop()

// SetLogger sets an optional logger for kkdai parser internals (ffmpeg stderr, debug signals).
func SetLogger(l zerolog.Logger) {
	if l.GetLevel() == zerolog.NoLevel {
		log = zerolog.Nop()
		return
	}
	log = l
}

func (s *Streamer) LinkStream(track *parsers.TrackParse, seekSec float64) (io.ReadCloser, func(), error) {
	return kkdaiLink(track, seekSec)
}
func (s *Streamer) PipeStream(track *parsers.TrackParse, seekSec float64) (io.ReadCloser, func(), error) {
	return kkdaiPipe(track, seekSec)
}
func (s *Streamer) SupportsPipe() bool {
	return true
}
