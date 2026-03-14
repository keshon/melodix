package stream

import (
	"errors"
	"io"
	"log"

	"github.com/keshon/melodix/pkg/music/parsers"
)

const maxRecoveryAttempts = 3

// RecoveryStream wraps a TrackStream and attempts to auto-recover on early stream termination.
type RecoveryStream struct {
	track       *parsers.TrackParse
	parserIndex int            // current parser index
	stream      *TrackStream   // active stream
	cleanup     func()         // cleanup function for the current stream
	seekSec     float64        // approximate playback position
	retries     map[string]int // parser => recovery attempts
	firstRead   bool           // used to detect immediate EOF at start
}

// NewRecoveryStream creates a new resilient wrapper for a track
func NewRecoveryStream(track *parsers.TrackParse) *RecoveryStream {
	return &RecoveryStream{
		track:     track,
		retries:   make(map[string]int),
		firstRead: true,
	}
}

// Open attempts to open a TrackStream for the current parser
func (rs *RecoveryStream) Open(seek float64) error {
	for i := rs.parserIndex; i < len(rs.track.SourceInfo.AvailableParsers); i++ {
		parser := rs.track.SourceInfo.AvailableParsers[i]

		if rs.retries[parser] >= maxRecoveryAttempts {
			log.Printf("[RecoveryStream] Parser %s exceeded max recovery attempts", parser)
			continue
		}

		stream, cleanup, err := openWithParser(rs.track, parser, seek)
		if err != nil {
			log.Printf("[RecoveryStream] Failed to open stream with parser %s: %v", parser, err)
			rs.retries[parser]++
			continue
		}

		rs.parserIndex = i
		rs.stream = stream
		rs.cleanup = cleanup
		rs.seekSec = seek
		rs.firstRead = true
		log.Printf("[RecoveryStream] Opened stream with parser %s at seek %.2f", parser, seek)
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
		log.Printf("[RecoveryStream] Max recovery attempts reached for parser %s", parser)
		return false
	}

	// Normalize duration (seconds)
	var durSec float64
	if rs.track.Duration > 0 {
		durSec = float64(rs.track.Duration) / float64(1e9) // if stored in ns
	}

	if durSec > 0 {
		if rs.seekSec < 0.95*durSec {
			log.Printf("[RecoveryStream] Early EOF detected (%.2f/%.2f), will attempt recovery", rs.seekSec, durSec)
			return true
		}
		return false
	}

	// No duration: only recover on immediate EOF
	if rs.firstRead || rs.seekSec < 1.0 {
		log.Printf("[RecoveryStream] Early EOF detected without duration, attempting recovery")
		return true
	}

	return false
}

// reopen cleans up the current stream and opens a new one at the current seek position.
func (rs *RecoveryStream) reopen() error {
	parser := rs.track.CurrentParser
	rs.retries[parser]++
	log.Printf("[RecoveryStream] Recovering stream for parser %s (attempt %d)...", parser, rs.retries[parser])

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

// GetTrack returns the underlying track
func (rs *RecoveryStream) GetTrack() *parsers.TrackParse {
	return rs.track
}

// GetParser returns the current parser used
func (rs *RecoveryStream) GetParser() string {
	if rs.stream != nil {
		return rs.stream.Parser
	}
	return ""
}
