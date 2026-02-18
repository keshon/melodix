package parsers

import "io"

type Streamer interface {
	GetLinkStream(track *TrackParse, seekSec float64) (io.ReadCloser, func(), error)
	GetPipeStream(track *TrackParse, seekSec float64) (io.ReadCloser, func(), error)
	SupportsPipe() bool
}
