// Package scnative streams SoundCloud tracks natively via api-v2 (no yt-dlp):
// resolve → pick transcoding → signed stream URL → ffmpeg. Diagnostics flow through
// the soundcloudapi package logger and ffmpeg stderr classification.
package scnative

import (
	"errors"
	"io"

	"github.com/keshon/melodix/pkg/music/parsers"
)

type Streamer struct{}

func (s *Streamer) LinkStream(track *parsers.TrackParse, seekSec float64) (io.ReadCloser, func(), error) {
	return scnativeLink(track, seekSec)
}
func (s *Streamer) PipeStream(track *parsers.TrackParse, seekSec float64) (io.ReadCloser, func(), error) {
	return nil, nil, errors.New("scnative: pipe streaming not supported")
}
func (s *Streamer) SupportsPipe() bool {
	return false
}
