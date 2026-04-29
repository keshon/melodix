// Package stream provides track stream opening, recovery, and PCM format constants.
package stream

import (
	"fmt"
	"io"
	"log"
	"strings"

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
	track  *parsers.TrackParse
	parser string
}

// Track returns the underlying track.
func (ts *TrackStream) Track() *parsers.TrackParse {
	return ts.track
}

// Parser returns the parser used for this stream.
func (ts *TrackStream) Parser() string {
	return ts.parser
}

// Registry maps parser names to streamer implementations
var Registry = map[string]parsers.Streamer{
	"ytdlp-link":  &ytdlp.Streamer{},
	"ytdlp-pipe":  &ytdlp.Streamer{},
	"kkdai-link":  &kkdai.Streamer{},
	"kkdai-pipe":  &kkdai.Streamer{},
	"ffmpeg-link": &ffmpeg.Streamer{},
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
	streamer, ok := Registry[parser]
	if !ok {
		return nil, nil, fmt.Errorf("streamer not found for parser: %s", parser)
	}

	var r io.ReadCloser
	var cleanup func()
	var err error

	if streamer.SupportsPipe() && strings.HasSuffix(parser, "-pipe") {
		r, cleanup, err = streamer.PipeStream(track, seekSec)
	} else {
		r, cleanup, err = streamer.LinkStream(track, seekSec)
	}

	if err != nil {
		return nil, cleanup, err
	}

	ts := &TrackStream{
		ReadCloser: r,
		track:      track,
		parser:     parser,
	}
	return ts, cleanup, nil
}
