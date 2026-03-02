package stream

import (
	"fmt"
	"io"
	"log"

	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/parsers/ffmpeg"
	"github.com/keshon/melodix/pkg/music/parsers/kkdai"
	"github.com/keshon/melodix/pkg/music/parsers/ytdlp"
)

const (
	Channels   = 2
	SampleRate = 48000
	FrameSize  = 960 // 20ms at 48kHz
)

// TrackStream wraps a track's PCM stream and metadata.
type TrackStream struct {
	io.ReadCloser
	Track  *parsers.TrackParse
	Parser string
}

// GetTrack returns the underlying track
func (ts *TrackStream) GetTrack() *parsers.TrackParse {
	return ts.Track
}

// GetParser returns the parser used for this stream
func (ts *TrackStream) GetParser() string {
	return ts.Parser
}

// StreamerRegistry maps parser names to streamer implementations
var StreamerRegistry = map[string]parsers.Streamer{
	"ytdlp-link":  &ytdlp.YTDLPStreamer{},
	"ytdlp-pipe":  &ytdlp.YTDLPStreamer{},
	"kkdai-link":  &kkdai.KKDAIStreamer{},
	"kkdai-pipe":  &kkdai.KKDAIStreamer{},
	"ffmpeg-link": &ffmpeg.FFMPEGStreamer{},
}

// OpenTrack attempts to open a stream for a track, trying parsers in order
func OpenTrack(track *parsers.TrackParse, seekSec float64) (*TrackStream, func(), string, error) {
	var errs []error
	var cleanup func()
	var lastParser string

	for _, parser := range track.SourceInfo.AvailableParsers {
		lastParser = parser
		stream, c, err := openWithParser(track, parser, seekSec)
		if err == nil {
			return stream, c, parser, nil
		}

		errs = append(errs, fmt.Errorf("[%s] %w", parser, err))
		cleanup = c
		log.Printf("Parser %s failed for track %s: %v, trying next parser...", parser, track.Title, err)
	}

	// Combine all parser errors
	var combinedErr string
	for _, e := range errs {
		combinedErr += e.Error() + "; "
	}

	return nil, cleanup, lastParser, fmt.Errorf("all parsers failed for track %s: %s", track.Title, combinedErr)
}

// openWithParser opens a stream using the specified parser
func openWithParser(track *parsers.TrackParse, parser string, seekSec float64) (*TrackStream, func(), error) {
	streamer, ok := StreamerRegistry[parser]
	if !ok {
		return nil, nil, fmt.Errorf("streamer not found for parser: %s", parser)
	}

	var r io.ReadCloser
	var cleanup func()
	var err error

	if streamer.SupportsPipe() && isPipeParser(parser) {
		r, cleanup, err = streamer.GetPipeStream(track, seekSec)
	} else {
		r, cleanup, err = streamer.GetLinkStream(track, seekSec)
	}

	if err != nil {
		return nil, cleanup, err
	}

	ts := &TrackStream{
		ReadCloser: r,
		Track:      track,
		Parser:     parser,
	}
	return ts, cleanup, nil
}

// isPipeParser returns true if the parser is a pipe parser
func isPipeParser(parser string) bool {
	return parser == "ytdlp-pipe" || parser == "kkdai-pipe"
}
