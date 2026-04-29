package stream

import (
	"errors"
	"io"

	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/rs/zerolog"
)

const maxRecoveryAttempts = 3

// RecoveryStream wraps a TrackStream and attempts to auto-recover on early stream termination.
// It covers flaky media (YouTube/ffmpeg reads), not Discord gateway or voice WebSocket loss;
// voice transport is handled by the player/sink layer (invalidate + rejoin).
type RecoveryStream struct {
	track       *parsers.TrackParse
	parserIndex int            // current parser index
	stream      *TrackStream   // active stream
	cleanup     func()         // cleanup function for the current stream
	seekSec     float64        // approximate playback position
	retries     map[string]int // parser => recovery attempts
	firstRead   bool           // used to detect immediate EOF at start
	log         zerolog.Logger
}

// NewRecoveryStream creates a new resilient wrapper for a track
func NewRecoveryStream(track *parsers.TrackParse) *RecoveryStream {
	return NewRecoveryStreamWithLogger(track, zerolog.Nop())
}

// NewRecoveryStreamWithLogger creates a new resilient wrapper for a track using the provided logger.
func NewRecoveryStreamWithLogger(track *parsers.TrackParse, log zerolog.Logger) *RecoveryStream {
	return &RecoveryStream{
		track:     track,
		retries:   make(map[string]int),
		firstRead: true,
		log:       log,
	}
}

// Open attempts to open a TrackStream for the current parser
func (rs *RecoveryStream) Open(seek float64) error {
	for i := rs.parserIndex; i < len(rs.track.SourceInfo.AvailableParsers); i++ {
		parser := rs.track.SourceInfo.AvailableParsers[i]

		if rs.retries[parser] >= maxRecoveryAttempts {
			rs.log.Warn().Str("parser", parser).Msg("parser_exceeded_recovery_attempts")
			continue
		}

		stream, cleanup, err := openWithParser(rs.track, parser, seek)
		if err != nil {
			rs.log.Warn().Str("parser", parser).Err(err).Msg("stream_open_failed")
			rs.retries[parser]++
			continue
		}

		rs.parserIndex = i
		rs.stream = stream
		rs.cleanup = cleanup
		rs.seekSec = seek
		rs.track.CurrentParser = parser
		rs.firstRead = true
		rs.log.Info().Str("parser", parser).Float64("seek", seek).Msg("stream_opened")
		return nil
	}

	return errors.New("all parsers failed or exceeded recovery attempts")
}

// Read implements io.Reader for RecoveryStream
func (rs *RecoveryStream) Read(p []byte) (int, error) {
	for {
		if rs.stream == nil {
			return 0, errors.New("stream not opened")
		}

		n, err := rs.stream.Read(p)
		rs.seekSec += float64(n) / (SampleRate * Channels * 2)

		// "Instant fail": stream opened but immediately errored/EOFs on first read.
		// In that case we advance to the next parser instead of retrying the same one.
		if rs.firstRead && err != nil {
			prevParser := rs.track.CurrentParser
			rs.retries[prevParser]++
			rs.log.Warn().Str("parser", prevParser).Err(err).Msg("immediate_failure_switching_parser")

			if rs.cleanup != nil {
				rs.cleanup()
				rs.cleanup = nil
			}
			if rs.stream != nil {
				_ = rs.stream.Close()
				rs.stream = nil
			}

			rs.parserIndex++
			if reopenErr := rs.Open(rs.seekSec); reopenErr != nil {
				return 0, err
			}
			continue
		}

		if err == io.EOF && n == 0 && rs.shouldRecover() {
			if reopenErr := rs.reopen(); reopenErr != nil {
				return 0, io.EOF
			}
			continue
		}

		rs.firstRead = false
		return n, err
	}
}

// shouldRecover decides if we need to attempt recovery
func (rs *RecoveryStream) shouldRecover() bool {
	parser := rs.track.CurrentParser

	// Already exceeded max attempts
	if rs.retries[parser] >= maxRecoveryAttempts {
		rs.log.Warn().Str("parser", parser).Msg("max_recovery_attempts_reached")
		return false
	}

	// Normalize duration (seconds)
	var durSec float64
	if rs.track.Duration > 0 {
		durSec = rs.track.Duration.Seconds()
	}

	if durSec > 0 {
		if rs.seekSec < 0.95*durSec {
			rs.log.Warn().Float64("seek", rs.seekSec).Float64("duration", durSec).Msg("early_eof_detected")
			return true
		}
		return false
	}

	// No duration: only recover on immediate EOF
	if rs.firstRead || rs.seekSec < 1.0 {
		rs.log.Warn().Float64("seek", rs.seekSec).Msg("early_eof_no_duration")
		return true
	}

	return false
}

// reopen cleans up the current stream and opens a new one at the current seek position.
func (rs *RecoveryStream) reopen() error {
	parser := rs.track.CurrentParser
	rs.retries[parser]++
	rs.log.Warn().Str("parser", parser).Int("attempt", rs.retries[parser]).Msg("recovering_stream")

	if rs.cleanup != nil {
		rs.cleanup()
		rs.cleanup = nil
	}
	if rs.stream != nil {
		_ = rs.stream.Close()
		rs.stream = nil
	}

	return rs.Open(rs.seekSec)
}

// ReopenAfterTransportFailure closes the media stream and reopens at the current approximate seek
// position (e.g. after Discord voice reconnect). Does not count toward parser EOF recovery limits.
func (rs *RecoveryStream) ReopenAfterTransportFailure() error {
	if rs.cleanup != nil {
		rs.cleanup()
		rs.cleanup = nil
	}
	if rs.stream != nil {
		_ = rs.stream.Close()
		rs.stream = nil
	}
	return rs.Open(rs.seekSec)
}

// Close closes the underlying stream. Safe to call multiple times (idempotent).
func (rs *RecoveryStream) Close() error {
	var err error
	if rs.cleanup != nil {
		rs.cleanup()
		rs.cleanup = nil
	}
	if rs.stream != nil {
		err = rs.stream.Close()
		rs.stream = nil
	}
	return err
}

// Track returns the underlying track.
func (rs *RecoveryStream) Track() *parsers.TrackParse {
	return rs.track
}

// Parser returns the current parser used.
func (rs *RecoveryStream) Parser() string {
	if rs.stream != nil {
		return rs.stream.Parser()
	}
	return ""
}
