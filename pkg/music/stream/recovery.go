package stream

import (
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/keshon/melodix/pkg/music/cache"
	"github.com/keshon/melodix/pkg/music/opus"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/rs/zerolog"
)

const (
	maxRecoveryAttempts = 3
	// liveReopenBackoff spaces out reconnects to a live source. Finite tracks
	// reopen immediately: there the delay is audible silence, while a live
	// stream has nothing to catch up on anyway.
	liveReopenBackoff = 500 * time.Millisecond
)

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

	// onParserConfirmed is called when a freshly opened stream yields its first
	// packet, i.e. when the active parser is proven to actually produce audio.
	// Fired from the ReadPacket goroutine; nil disables the notification.
	onParserConfirmed func(parser string)

	// closed is set by Close so a read that fails because the stream was torn
	// down is not mistaken for a recoverable one. It is atomic because the
	// read-ahead buffer drives ReadPacket from its own goroutine while Close
	// runs on the playback goroutine.
	closed atomic.Bool

	// buffered is the anti-skip view handed to the sink; see Packets.
	buffered *opus.BufferedReader

	// mu guards reader and cleanup — the only fields Close touches while the
	// read-ahead producer may still be running. Every other field belongs to the
	// producer goroutine alone, and Close reaches them only after Wait proves it
	// has exited. The lock is never held across a blocking read or a parser
	// Open, so a stalled source cannot block teardown.
	mu sync.Mutex

	fromCache     bool          // the active stream is served from the track cache
	cacheDisabled bool          // cache failed for this stream; don't try it again
	cacheWriter   *cache.Writer // active write-through blob (nil = not caching)
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

// SetOnParserConfirmed registers a callback fired when a stream first yields a
// packet (see confirmOpen). Call before the first ReadPacket; not safe to change
// once packets are flowing.
func (rs *RecoveryStream) SetOnParserConfirmed(fn func(parser string)) {
	rs.onParserConfirmed = fn
}

// Open acquires the packet stream for the current parser, advancing through the
// track's parser list past any that fail or exhausted their recovery budget.
// A successful Open is NOT proof that audio will flow: the ffmpeg-backed parsers
// only spawn a process here, so a CDN 403 surfaces later, on the first read.
// confirmOpen is where a parser is known to be playing.
func (rs *RecoveryStream) Open(seek float64) error {
	// Cache-first: serve a completed blob for this track if one exists (shared
	// across guilds). A miss or open failure falls through to the parser list.
	if activeCache != nil && !rs.cacheDisabled {
		if key, ok := cache.Key(rs.track); ok && activeCache.Has(key) {
			reader, err := activeCache.OpenAt(key, opus.SeekPackets(seek))
			if err != nil {
				rs.log.Warn().Str("cache_key", key).Err(err).Msg("cache_open_failed")
				rs.cacheDisabled = true
			} else {
				rs.setActive(reader, func() { _ = reader.Close() })
				rs.seekSec = seek
				rs.curParser = ""
				rs.track.CurrentParser = ""
				rs.track.Passthrough = true
				rs.track.Cached = true
				rs.fromCache = true
				rs.firstRead = true
				rs.log.Info().Str("cache_key", key).Float64("seek", seek).Msg("stream_opening_from_cache")
				return nil
			}
		}
	}

	for i := rs.parserIndex; i < len(rs.track.SourceInfo.AvailableParsers); i++ {
		parser := rs.track.SourceInfo.AvailableParsers[i]
		if rs.retries[parser] >= maxRecoveryAttempts {
			rs.log.Warn().Str("parser", parser).Msg("parser_exceeded_recovery_attempts")
			continue
		}
		rs.track.Passthrough = false // parser sets true if it opens passthrough
		rs.track.Cached = false
		reader, cleanup, err := openWithParser(rs.track, parser, seek)
		if err != nil {
			rs.log.Warn().Str("parser", parser).Err(err).Msg("stream_open_failed")
			rs.retries[parser]++
			continue
		}
		rs.startCacheWrite(seek)
		rs.parserIndex = i
		rs.setActive(reader, cleanup)
		rs.seekSec = seek
		rs.curParser = parser
		rs.track.CurrentParser = parser
		rs.fromCache = false
		rs.firstRead = true
		rs.log.Info().Str("parser", parser).Float64("seek", seek).Msg("stream_opening")
		return nil
	}
	return errors.New("all parsers failed or exceeded recovery attempts")
}

// startCacheWrite begins caching a clean from-start play of an as-yet-uncached
// track (once per stream). Writing happens in ReadPacket, above the recovery
// logic, so a single blob spans parser switches and transport reopens; it is
// committed only on the final natural EOF (see commitCache).
func (rs *RecoveryStream) startCacheWrite(seek float64) {
	if rs.cacheWriter != nil || activeCache == nil || seek != 0 {
		return
	}
	key, ok := cache.Key(rs.track)
	if !ok || activeCache.Has(key) {
		return
	}
	w, err := activeCache.NewWriter(key, cache.Meta{Source: rs.track.SourceInfo.SourceName, Title: rs.track.Title})
	if err != nil {
		rs.log.Warn().Str("cache_key", key).Err(err).Msg("cache_writer_failed")
		return
	}
	rs.cacheWriter = w
	rs.log.Info().Str("cache_key", key).Msg("cache_write_started")
}

// commitCache finalizes the cached blob after the track ended cleanly.
func (rs *RecoveryStream) commitCache() {
	if rs.cacheWriter == nil {
		return
	}
	if err := rs.cacheWriter.Commit(); err != nil {
		rs.log.Warn().Err(err).Msg("cache_commit_failed")
	} else {
		rs.log.Info().Msg("cache_write_committed")
	}
	rs.cacheWriter = nil
}

// abortCache discards a partial blob (stop/skip before the end, or a failure).
func (rs *RecoveryStream) abortCache() {
	if rs.cacheWriter == nil {
		return
	}
	_ = rs.cacheWriter.Abort()
	rs.cacheWriter = nil
}

// ReadPacket returns the next 20ms Opus packet, applying recovery: an immediate
// failure advances to the next parser; an early EOF reopens the same parser at
// the current position.
func (rs *RecoveryStream) ReadPacket() ([]byte, error) {
	for {
		rs.mu.Lock()
		reader := rs.reader
		rs.mu.Unlock()
		if reader == nil {
			return nil, errors.New("stream not opened")
		}
		pkt, err := reader.ReadPacket()
		if err == nil {
			if rs.firstRead {
				rs.firstRead = false
				rs.confirmOpen()
			}
			rs.seekSec += float64(opus.FrameMs) / 1000
			if rs.cacheWriter != nil {
				if werr := rs.cacheWriter.Write(pkt); werr != nil {
					rs.log.Warn().Err(werr).Msg("cache_write_failed")
					rs.abortCache()
				}
			}
			return pkt, nil
		}

		// "Instant fail": errored on the very first read.
		if rs.firstRead {
			// A cache read that fails immediately (missing/corrupt blob): drop the
			// cache and retry the real parser list from the current index — do NOT
			// advance parserIndex, since the cache is not one of its entries.
			if rs.fromCache {
				rs.log.Warn().Err(err).Msg("cache_immediate_failure_falling_back")
				rs.cacheDisabled = true
				rs.fromCache = false
				rs.closeCurrent()
				if reopenErr := rs.Open(rs.seekSec); reopenErr != nil {
					rs.abortCache()
					return nil, err
				}
				continue
			}
			// Otherwise advance to the next parser.
			rs.retries[rs.curParser]++
			rs.log.Warn().Str("parser", rs.curParser).Err(err).Msg("immediate_failure_switching_parser")
			rs.closeCurrent()
			rs.parserIndex++
			if reopenErr := rs.Open(rs.seekSec); reopenErr != nil {
				rs.abortCache()
				return nil, err
			}
			continue
		}

		// The media stopped arriving before the track's end → reopen the same
		// parser at the current position. Two error shapes mean exactly that,
		// and both have to be caught:
		//
		//   - io.EOF, the body ending short. This is what parsers fronted by
		//     ffmpeg surface, because ffmpeg turns a dropped connection into a
		//     closed pipe on its side.
		//   - a transport error, the CDN resetting the connection mid-track.
		//     The passthrough path surfaces these raw, with nothing between it
		//     and the socket, so a reset arrives as "connection forcibly
		//     closed" rather than as EOF. Treating those as terminal was a gap
		//     that opened when passthrough removed the ffmpeg layer: the track
		//     died at the reset instead of resuming a second later.
		//
		// A cache blob is only committed on a clean EOF, so it is always
		// complete: a cache EOF is a natural end, never an early one.
		if !rs.closed.Load() && !rs.fromCache && rs.shouldRecover(err) {
			if reopenErr := rs.reopen(err); reopenErr != nil {
				rs.abortCache()
				return nil, err
			}
			continue
		}

		// Terminal: a clean EOF means the track ended → commit the cache; any
		// other error means we gave up → discard the partial.
		if errors.Is(err, io.EOF) {
			rs.commitCache()
		} else {
			rs.abortCache()
		}
		return nil, err
	}
}

// confirmOpen reports that the active stream just produced its first packet --
// the point at which the parser is known to be playing rather than merely
// opened. It fires again after every reopen (parser switch, early-EOF recovery,
// transport reopen), so the parser it names is always the live one.
func (rs *RecoveryStream) confirmOpen() {
	if rs.fromCache {
		rs.log.Info().Float64("seek", rs.seekSec).Msg("stream_opened_from_cache")
	} else {
		rs.log.Info().Str("parser", rs.curParser).Float64("seek", rs.seekSec).Msg("stream_opened")
	}
	if rs.onParserConfirmed != nil {
		rs.onParserConfirmed(rs.curParser)
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

// isLive reports whether the track has no fixed end. Internet radio is the
// obvious case; a YouTube live broadcast is the other, and both arrive here the
// same way — as a track whose duration nobody could fill in.
func (rs *RecoveryStream) isLive() bool {
	return rs.track.Duration <= 0
}

// shouldRecover reports whether a failed read looks like an early end worth
// reopening. cause is carried only so the log names the real reason.
func (rs *RecoveryStream) shouldRecover(cause error) bool {
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
			rs.log.Warn().Float64("seek", rs.seekSec).Float64("duration", durSec).Err(cause).Msg("early_stream_end_detected")
			return true
		}
		return false
	}
	// A live stream has no natural end to compare against, so every stop is an
	// interruption rather than a finish — there is no such thing as "near the
	// end" here. Recover, and let the attempt budget checked above be what stops
	// this from becoming an endless reconnect against a station that has genuinely
	// gone off air.
	rs.log.Warn().Float64("played", rs.seekSec).Err(cause).Msg("live_stream_interrupted")
	return true
}

func (rs *RecoveryStream) reopen(cause error) error {
	rs.retries[rs.curParser]++

	// A live stream has no position to go back to: rejoining means picking up at
	// the current edge, not replaying to where the listener had got to.
	seek := rs.seekSec
	if rs.isLive() {
		seek = 0
	}

	rs.log.Warn().Str("parser", rs.curParser).Int("attempt", rs.retries[rs.curParser]).
		Float64("seek", seek).Bool("live", rs.isLive()).Err(cause).Msg("recovering_stream")
	rs.closeCurrent()

	if rs.isLive() {
		// Reconnecting to a source that just dropped the connection rarely works
		// in the same millisecond, and for live there is no buffered position to
		// lose by waiting.
		time.Sleep(liveReopenBackoff)
	}
	return rs.Open(seek)
}

// ReopenAfterTransportFailure reopens the media stream at the current position
// (e.g. after a Discord voice reconnect); does not count against parser recovery.
func (rs *RecoveryStream) ReopenAfterTransportFailure() error {
	rs.closeCurrent()
	return rs.Open(rs.seekSec)
}

// setActive installs the freshly opened stream under the lock Close also takes.
func (rs *RecoveryStream) setActive(reader opus.Reader, cleanup func()) {
	rs.mu.Lock()
	rs.reader = reader
	rs.cleanup = cleanup
	rs.mu.Unlock()
}

func (rs *RecoveryStream) closeCurrent() {
	rs.mu.Lock()
	cleanup := rs.cleanup
	rs.cleanup = nil
	rs.reader = nil
	rs.mu.Unlock()

	// Outside the lock: tearing a socket down can block, and a producer parked
	// in ReadPacket is unblocked by exactly this call.
	if cleanup != nil {
		cleanup()
	}
}

// Packets returns the packet stream the sink should consume: this stream
// wrapped in the anti-skip read-ahead buffer, when one is configured.
//
// The buffer belongs above recovery, not below it. Wrapped around the parser's
// reader instead, a reopen tears the buffer down together with the stream it
// wraps, and the consumer sits blocked inside ReadPacket for the whole
// reconnect — so the gap is fully audible, which is precisely what an anti-skip
// buffer exists to prevent. Above, the queued lead keeps playing while the
// reopen happens underneath, and it survives transport reopens too.
//
// Call once, after Open; the result is cached and owned by this stream.
func (rs *RecoveryStream) Packets() opus.Reader {
	if rs.buffered != nil {
		return rs.buffered
	}
	wrapped, ok := bufferWrapReader(packetView{rs})
	if !ok {
		return packetView{rs}
	}
	rs.buffered = wrapped
	return wrapped
}

// Close releases the active stream. Safe to call multiple times.
func (rs *RecoveryStream) Close() error {
	// Order matters. closed first, so a read failing because of this teardown is
	// not mistaken for an early end worth reopening. Then Stop, which only
	// signals — the producer parked in ReadPacket is unblocked by closing the
	// source underneath it, which closeCurrent does last.
	rs.closed.Store(true)
	if rs.buffered != nil {
		rs.buffered.Stop()
	}
	// Unblocks a producer parked in a read, so the goroutine can notice it is
	// stopped and exit.
	rs.closeCurrent()
	if rs.buffered != nil {
		rs.buffered.Wait()
	}
	// Past this point the producer is gone, so the rest needs no lock.
	rs.abortCache() // stopped before the end → discard the partial (no-op if committed)
	return nil
}

// Track returns the underlying track.
func (rs *RecoveryStream) Track() *parsers.Track { return rs.track }

// Parser returns the current parser key.
func (rs *RecoveryStream) Parser() string { return rs.curParser }
