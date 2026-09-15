package player

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/keshon/melodix/pkg/music/opus"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/sink"
	"github.com/keshon/melodix/pkg/music/sources"
	"github.com/keshon/melodix/pkg/music/stream"
)

// What these cover is the seam between two halves that were each already
// tested and never tested together.
//
// A sink reporting stream.ErrVoiceTransport had no producer at all under
// disgo -- OpusFrameProvider.Close is the only signal the library offers for a
// connection dying under a running track, and v0.19.6 never calls it -- so
// runPlayback's transport branch had not executed since the migration.
// Restoring the signal made roughly forty lines live again in one commit:
// the rejoin, the hard/soft mode choice, the attempt budget, the backoff and
// the reopen handshake. voicesink proves the error gets raised, and stream
// proves a reopen is safe against the read-ahead goroutine; nothing proved
// the player does the right thing between them.

// transportSink fails its first failures Stream calls with ErrVoiceTransport
// and drains afterwards, which is a voice connection dying mid-track and then
// coming back.
//
// It plays a little before failing on purpose. A transport that dies before a
// single packet has moved would leave the stream at position zero, where a
// reopen that restarted the track and one that resumed it look identical.
type transportSink struct {
	failing    atomic.Int64
	streams    atomic.Int64
	playBefore int
}

func newTransportSink(failures int) *transportSink {
	s := &transportSink{playBefore: 5}
	s.failing.Store(int64(failures))
	return s
}

func (s *transportSink) Stream(r opus.Reader, stop <-chan struct{}) error {
	s.streams.Add(1)

	for i := 0; i < s.playBefore; i++ {
		select {
		case <-stop:
			return stream.ErrPlaybackStopped
		default:
		}
		if _, err := r.ReadPacket(); err != nil {
			return nil
		}
	}

	if s.failing.Add(-1) >= 0 {
		return stream.ErrVoiceTransport
	}

	for {
		select {
		case <-stop:
			return stream.ErrPlaybackStopped
		default:
		}
		if _, err := r.ReadPacket(); err != nil {
			return nil
		}
	}
}

// transportProvider counts sink invalidations, because whether one happens is
// the whole difference between hard and soft recovery.
type transportProvider struct {
	sink        sink.AudioSink
	invalidated atomic.Int64
}

func (p *transportProvider) Sink(string) (sink.AudioSink, error) { return p.sink, nil }
func (p *transportProvider) ReleaseSink(string)                  {}
func (p *transportProvider) InvalidateSink()                     { p.invalidated.Add(1) }

// openLedger records every open the registry was asked for and the position it
// was asked to start at. A reopen is not observable any other way: it is
// served by whichever goroutine is reading, and the only thing it leaves
// behind is a second call to the parser.
type openLedger struct {
	mu    sync.Mutex
	seeks []float64
}

func (l *openLedger) add(seek float64) {
	l.mu.Lock()
	l.seeks = append(l.seeks, seek)
	l.mu.Unlock()
}

func (l *openLedger) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.seeks)
}

func (l *openLedger) at(i int) float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	if i >= len(l.seeks) {
		return -1
	}
	return l.seeks[i]
}

// ledgerStreamer serves a fixed number of packets per open and reports where
// each open was asked to start.
//
// Duration is set short so that an EOF is a natural end rather than an early
// one: media recovery would otherwise reopen the stream too, and a test of
// transport recovery would be counting both.
func ledgerStreamer(l *openLedger, packets int) fakeStreamer {
	return fakeStreamer{
		open: func(track *parsers.Track, seek float64) (opus.Reader, func(), error) {
			l.add(seek)
			track.Title = "t"
			track.Duration = time.Millisecond
			return &pacedReader{left: packets}, func() {}, nil
		},
	}
}

func withBufferAhead(t *testing.T, ms int) {
	t.Helper()
	stream.SetBufferAhead(ms)
	t.Cleanup(func() { stream.SetBufferAhead(0) })
}

// newTestPlayer builds a player that is stopped when the test ends.
//
// Stopping it is not tidiness. A test returns as soon as the thing it asserts
// has happened, with a track still playing, and the playback goroutine reads
// the read-ahead depth every time it starts one -- so a cleanup that restores
// that global races the run still using it. Registered after the caller's own
// cleanups so it runs before them, which is what makes restoring a global
// safe.
func newTestPlayer(t *testing.T, provider sink.Provider, opts Options) *Player {
	t.Helper()
	p := NewWithOptions(provider, nil, opts)
	t.Cleanup(func() {
		_ = p.Stop(true)
		waitFor(t, 10*time.Second, func() bool { return !p.IsPlaying() })
	})
	return p
}

// The wedge this replaced: a voice connection dying mid-track left Stream
// blocked forever, so the track never ended and the queue never moved. The
// track advancing is every part of the chain working -- the sink reported the
// transport gone, the player asked the stream to reopen, the reading goroutine
// served the request, and the track ran to its end.
func TestATransportFailureReopensTheStreamAndPlaysOn(t *testing.T) {
	opens := &openLedger{}
	swapRegistry(t, map[string]parsers.Streamer{"plays": ledgerStreamer(opens, 40)})

	provider := &transportProvider{sink: newTransportSink(1)}
	p := newTestPlayer(t, provider, Options{})

	tracks := []sources.TrackInfo{testTrack("first", "plays"), testTrack("second", "plays")}
	if err := p.EnqueueTrackInfos(tracks); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := p.PlayNext("chan"); err != nil {
		t.Fatalf("play: %v", err)
	}

	// Three opens: the first track, its reopen after the transport failure,
	// and the track the queue moved on to once it finished.
	waitFor(t, 15*time.Second, func() bool { return opens.count() >= 3 })

	if got := opens.count(); got != 3 {
		t.Fatalf("opens = %d, want 3 (start, reopen, next track)", got)
	}
	if got := provider.invalidated.Load(); got != 1 {
		t.Fatalf("InvalidateSink called %d times, want 1: hard recovery rejoins", got)
	}
}

// A reopen resumes the track rather than restarting it. Restarting is not a
// small difference: on a link that drops often enough to need this, a track
// that begins again on every drop never finishes.
func TestATransportReopenResumesWhereTheTrackStopped(t *testing.T) {
	opens := &openLedger{}
	swapRegistry(t, map[string]parsers.Streamer{"plays": ledgerStreamer(opens, 200)})

	provider := &transportProvider{sink: newTransportSink(1)}
	p := newTestPlayer(t, provider, Options{})

	if err := p.EnqueueTrackInfos([]sources.TrackInfo{testTrack("t", "plays")}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := p.PlayNext("chan"); err != nil {
		t.Fatalf("play: %v", err)
	}

	waitFor(t, 15*time.Second, func() bool { return opens.count() >= 2 })

	if first := opens.at(0); first != 0 {
		t.Fatalf("the track started at %v, want 0", first)
	}
	if resumed := opens.at(1); resumed <= 0 {
		t.Fatalf("the reopen started at %v, want a position past the start", resumed)
	}
}

// The configuration the bot actually ships: BUFFER_AHEAD_MS defaults to thirty
// seconds, which puts the reading goroutine somewhere other than the one
// runPlayback is on. That is the whole reason a reopen is a request rather
// than a call, and with the buffer off -- which every other test here runs
// with -- the request is served by the same goroutine that made it and the
// handshake is never exercised.
func TestTransportRecoveryWorksWithTheAntiSkipBufferOn(t *testing.T) {
	withBufferAhead(t, 600)

	opens := &openLedger{}
	swapRegistry(t, map[string]parsers.Streamer{"plays": ledgerStreamer(opens, 60)})

	provider := &transportProvider{sink: newTransportSink(1)}
	p := newTestPlayer(t, provider, Options{})

	tracks := []sources.TrackInfo{testTrack("first", "plays"), testTrack("second", "plays")}
	if err := p.EnqueueTrackInfos(tracks); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := p.PlayNext("chan"); err != nil {
		t.Fatalf("play: %v", err)
	}

	waitFor(t, 20*time.Second, func() bool { return opens.count() >= 3 })
}

// Hard recovery rejoins the voice channel before reopening the stream, which
// is the safe default: the usual reason a transport died is that the
// connection carrying it is gone.
func TestHardRecoveryInvalidatesTheSinkOnEveryAttempt(t *testing.T) {
	opens := &openLedger{}
	swapRegistry(t, map[string]parsers.Streamer{"plays": ledgerStreamer(opens, 60)})

	provider := &transportProvider{sink: newTransportSink(2)}
	p := newTestPlayer(t, provider, Options{TransportRecoveryMode: RecoveryHard})

	if err := p.EnqueueTrackInfos([]sources.TrackInfo{testTrack("t", "plays")}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := p.PlayNext("chan"); err != nil {
		t.Fatalf("play: %v", err)
	}

	waitFor(t, 20*time.Second, func() bool { return opens.count() >= 3 })

	if got := provider.invalidated.Load(); got != 2 {
		t.Fatalf("InvalidateSink called %d times, want 2 (once per failure)", got)
	}
}

// Soft recovery spends its budget reopening the stream before it gives up on
// the connection, for the case where the connection is fine and the media is
// not. The budget is what stops it retrying a dead connection forever: past
// TransportSoftAttempts it rejoins like hard recovery does.
func TestSoftRecoveryReopensBeforeItInvalidatesTheSink(t *testing.T) {
	opens := &openLedger{}
	swapRegistry(t, map[string]parsers.Streamer{"plays": ledgerStreamer(opens, 60)})

	provider := &transportProvider{sink: newTransportSink(2)}
	p := newTestPlayer(t, provider, Options{
		TransportRecoveryMode: RecoverySoft,
		TransportSoftAttempts: 1,
	})

	if err := p.EnqueueTrackInfos([]sources.TrackInfo{testTrack("t", "plays")}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := p.PlayNext("chan"); err != nil {
		t.Fatalf("play: %v", err)
	}

	waitFor(t, 20*time.Second, func() bool { return opens.count() >= 3 })

	// Two failures, one soft attempt: the first reopens the stream alone, the
	// second falls back to rejoining.
	if got := provider.invalidated.Load(); got != 1 {
		t.Fatalf("InvalidateSink called %d times, want 1: the first attempt is soft", got)
	}
}

// A transport that never comes back must stop, and stop without walking the
// queue: nothing is wrong with the tracks in it, so playing the next one
// against the same dead connection would burn the whole queue and say the
// tracks failed.
func TestATransportThatNeverComesBackGivesUpWithoutBurningTheQueue(t *testing.T) {
	opens := &openLedger{}
	swapRegistry(t, map[string]parsers.Streamer{"plays": ledgerStreamer(opens, 60)})

	dead := newTransportSink(maxVoiceTransportAttempts + 5)
	provider := &transportProvider{sink: dead}

	failed := make(chan error, 1)
	p := newTestPlayer(t, provider, Options{
		OnPlaybackFailed: func(_ string, _ parsers.Track, err error) {
			select {
			case failed <- err:
			default:
			}
		},
	})
	// The failure callback is wired to a guild, and is skipped without one.
	p.SetGuildID("guild")

	tracks := []sources.TrackInfo{testTrack("first", "plays"), testTrack("second", "plays")}
	if err := p.EnqueueTrackInfos(tracks); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := p.PlayNext("chan"); err != nil {
		t.Fatalf("play: %v", err)
	}

	select {
	case err := <-failed:
		if !errors.Is(err, stream.ErrVoiceTransport) {
			t.Fatalf("reported %v, want a voice transport failure", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("a transport that never recovered was never reported as failed")
	}

	if got := dead.streams.Load(); got != int64(maxVoiceTransportAttempts) {
		t.Fatalf("streamed %d times, want %d: the attempt budget is what bounds this",
			got, maxVoiceTransportAttempts)
	}

	// The second track must still be waiting rather than have been played
	// against the same dead connection.
	if p.IsPlaying() {
		t.Fatal("the player is still playing after giving up on the transport")
	}
	if q := p.Queue(); len(q) != 1 || q[0].Title != "second" {
		t.Fatalf("queue = %v, want the untried second track still in it", q)
	}
}
