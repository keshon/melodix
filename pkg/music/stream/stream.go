// Package stream provides track stream opening, recovery, and PCM format constants.
package stream

import (
	"fmt"
	"io"

	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/parsers/ffmpeg"
	"github.com/keshon/melodix/pkg/music/parsers/kkdai"
	"github.com/keshon/melodix/pkg/music/parsers/scnative"
	"github.com/keshon/melodix/pkg/music/parsers/ytdlp"
	"github.com/keshon/melodix/pkg/music/parsers/ytnative"
	"github.com/keshon/melodix/pkg/music/sources"
	"github.com/rs/zerolog"
)

const (
	Channels   = 2
	SampleRate = 48000
	FrameSize  = 960 // 20ms at 48kHz
)

// TrackStream wraps a track's PCM stream and metadata.
type TrackStream struct {
	io.ReadCloser
	track  *parsers.Track
	parser string
}

// Track returns the underlying track.
func (ts *TrackStream) Track() *parsers.Track {
	return ts.track
}

// Parser returns the parser used for this stream.
func (ts *TrackStream) Parser() string {
	return ts.parser
}

// Entry describes a registered parser: the streamer implementation and whether
// this key opens via the pipe path (Go-side download piped into ffmpeg) or the
// link path (ffmpeg fetches the URL itself).
type Entry struct {
	Streamer parsers.Streamer
	UsePipe  bool
}

// Registry maps parser keys (see the sources.Parser* constants; the strings are
// persisted in playback history, so keys are frozen identifiers) to entries.
var Registry = map[string]Entry{
	sources.ParserYtnativeLink: {Streamer: &ytnative.Streamer{}},
	sources.ParserScnativeLink: {Streamer: &scnative.Streamer{}},
	sources.ParserKkdaiLink:    {Streamer: &kkdai.Streamer{}},
	sources.ParserKkdaiPipe:    {Streamer: &kkdai.Streamer{}, UsePipe: true},
	sources.ParserYtdlpLink:    {Streamer: &ytdlp.Streamer{}},
	sources.ParserYtdlpPipe:    {Streamer: &ytdlp.Streamer{}, UsePipe: true},
	sources.ParserFFmpegLink:   {Streamer: &ffmpeg.Streamer{}},
}

// OpenTrack attempts to open a stream for a track, trying parsers in order
func OpenTrack(track *parsers.Track, seekSec float64) (*TrackStream, func(), string, error) {
	return OpenTrackWithLogger(zerolog.Nop(), track, seekSec)
}

// OpenTrackWithLogger is like OpenTrack but logs parser fallbacks using the provided logger.
func OpenTrackWithLogger(log zerolog.Logger, track *parsers.Track, seekSec float64) (*TrackStream, func(), string, error) {
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
		log.Warn().Str("parser", parser).Str("title", track.Title).Err(err).Msg("parser_failed_try_next")
	}

	// Combine all parser errors
	var combinedErr string
	for _, e := range errs {
		combinedErr += e.Error() + "; "
	}

	return nil, cleanup, lastParser, fmt.Errorf("all parsers failed for track %s: %s", track.Title, combinedErr)
}

// openWithParser opens a stream using the specified parser
func openWithParser(track *parsers.Track, parser string, seekSec float64) (*TrackStream, func(), error) {
	entry, ok := Registry[parser]
	if !ok {
		return nil, nil, fmt.Errorf("streamer not found for parser: %s", parser)
	}

	var r io.ReadCloser
	var cleanup func()
	var err error

	if entry.UsePipe {
		r, cleanup, err = entry.Streamer.PipeStream(track, seekSec)
	} else {
		r, cleanup, err = entry.Streamer.LinkStream(track, seekSec)
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
