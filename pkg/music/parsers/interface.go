package parsers

import "io"

type Streamer interface {
	LinkStream(track *TrackParse, seekSec float64) (io.ReadCloser, func(), error)
	PipeStream(track *TrackParse, seekSec float64) (io.ReadCloser, func(), error)
	SupportsPipe() bool
}
