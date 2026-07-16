package stream

import (
	"errors"
	"io"

	"github.com/keshon/melodix/pkg/music/opus"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/rs/zerolog"
)

const maxRecoveryAttempts = 3

// RecoveryStream wraps a parser's Opus packet stream and auto-recovers on early
// termination (flaky media, not Discord transport — that's handled by the
// player/sink layer). It is the engine's packet source: ReadPacket yields the
// next 20ms Opus packet with recovery applied; Read/Close expose the same stream
// as decoded PCM (io.ReadCloser) for consumers that still want samples.
type RecoveryStream struct {
	track       *parsers.Track
	parserIndex int
	reader      opus.Reader    // active packet stream
	cleanup     func()         // cleanup for the active stream
	curParser   string         // registry key of the active parser
	seekSec     float64        // approximate playback position (packets × 20ms)
	retries     map[string]int // parser => recovery attempts
	firstRead   bool           // detect immediate failure at start
	pcm         io.ReadCloser  // lazily-built decode view (Read)
	log         zerolog.Logger
}

// NewRecoveryStream creates a resilient wrapper for a track.
func NewRecoveryStream(track *parsers.Track) *RecoveryStream {
	return NewRecoveryStreamWithLogger(track, zerolog.Nop())
}

// NewRecoveryStreamWithLogger creates a resilient wrapper using the given logger.
func NewRecoveryStreamWithLogger(track *parsers.Track, log zerolog.Logger) *RecoveryStream {
	return &RecoveryStream{
		track:     track,
		retries:   make(map[string]int),
		firstRead: true,
		log:       log,
	}
}

// Open opens the packet stream for the current parser, advancing through the
// track's parser list past any that fail or exhausted their recovery budget.
func (rs *RecoveryStream) Open(seek float64) error {
	for i := rs.parserIndex; i < len(rs.track.SourceInfo.AvailableParsers); i++ {
		parser := rs.track.SourceInfo.AvailableParsers[i]
		if rs.retries[parser] >= maxRecoveryAttempts {
			rs.log.Warn().Str("parser", parser).Msg("parser_exceeded_recovery_attempts")
			continue
		}
		rs.track.Passthrough = false // parser sets true if it opens passthrough
		reader, cleanup, err := openWithParser(rs.track, parser, seek)
		if err != nil {
			rs.log.Warn().Str("parser", parser).Err(err).Msg("stream_open_failed")
			rs.retries[parser]++
			continue
		}
		rs.parserIndex = i
		rs.reader = reader
		rs.cleanup = cleanup
		rs.seekSec = seek
		rs.curParser = parser
		rs.track.CurrentParser = parser
		rs.firstRead = true
		rs.log.Info().Str("parser", parser).Float64("seek", seek).Msg("stream_opened")
		return nil
	}
	return errors.New("all parsers failed or exceeded recovery attempts")
}

// ReadPacket returns the next 20ms Opus packet, applying recovery: an immediate
// failure advances to the next parser; an early EOF reopens the same parser at
// the current position.
func (rs *RecoveryStream) ReadPacket() ([]byte, error) {
	for {
		if rs.reader == nil {
			return nil, errors.New("stream not opened")
		}
		pkt, err := rs.reader.ReadPacket()
		if err == nil {
			rs.firstRead = false
			rs.seekSec += float64(opus.FrameMs) / 1000
			return pkt, nil
		}

		// "Instant fail": errored on the very first read → try the next parser.
		if rs.firstRead {
			rs.retries[rs.curParser]++
			rs.log.Warn().Str("parser", rs.curParser).Err(err).Msg("immediate_failure_switching_parser")
			rs.closeCurrent()
			rs.parserIndex++
			if reopenErr := rs.Open(rs.seekSec); reopenErr != nil {
				return nil, err
			}
			continue
		}

		// Early EOF before the track's end → reopen the same parser at seek.
		if errors.Is(err, io.EOF) && rs.shouldRecover() {
			if reopenErr := rs.reopen(); reopenErr != nil {
				return nil, io.EOF
			}
			continue
		}

		return nil, err
	}
}

// Read exposes the recovered packet stream as decoded PCM (s16le, 48kHz stereo).
func (rs *RecoveryStream) Read(p []byte) (int, error) {
	if rs.pcm == nil {
		rs.pcm = opus.DecodeReader(packetView{rs})
	}
	return rs.pcm.Read(p)
}

// packetView adapts RecoveryStream as an opus.Reader without owning its
// lifecycle (Close is a no-op; RecoveryStream.Close cleans up the real stream).
type packetView struct{ rs *RecoveryStream }

func (v packetView) ReadPacket() ([]byte, error) { return v.rs.ReadPacket() }
func (v packetView) Close() error                { return nil }

func (rs *RecoveryStream) shouldRecover() bool {
	if rs.retries[rs.curParser] >= maxRecoveryAttempts {
		rs.log.Warn().Str("parser", rs.curParser).Msg("max_recovery_attempts_reached")
		return false
	}
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
	// No duration: only recover on immediate EOF.
	if rs.firstRead || rs.seekSec < 1.0 {
		rs.log.Warn().Float64("seek", rs.seekSec).Msg("early_eof_no_duration")
		return true
	}
	return false
}

func (rs *RecoveryStream) reopen() error {
	rs.retries[rs.curParser]++
	rs.log.Warn().Str("parser", rs.curParser).Int("attempt", rs.retries[rs.curParser]).Msg("recovering_stream")
	rs.closeCurrent()
	return rs.Open(rs.seekSec)
}

// ReopenAfterTransportFailure reopens the media stream at the current position
// (e.g. after a Discord voice reconnect); does not count against parser recovery.
func (rs *RecoveryStream) ReopenAfterTransportFailure() error {
	rs.closeCurrent()
	return rs.Open(rs.seekSec)
}

func (rs *RecoveryStream) closeCurrent() {
	if rs.cleanup != nil {
		rs.cleanup()
		rs.cleanup = nil
	}
	rs.reader = nil
}

// Close releases the active stream. Safe to call multiple times.
func (rs *RecoveryStream) Close() error {
	rs.closeCurrent()
	return nil
}

// Track returns the underlying track.
func (rs *RecoveryStream) Track() *parsers.Track { return rs.track }

// Parser returns the current parser key.
func (rs *RecoveryStream) Parser() string { return rs.curParser }
