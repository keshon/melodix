package stream

import (
	"math"
	"sync"
	"testing"
	"time"

	"github.com/keshon/melodix/pkg/music/opus"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/sources"
)

// endlessReader never runs out, so the read-ahead producer stays live for the
// whole test rather than finishing before the reopens land.
type endlessReader struct{ closed bool }

func (r *endlessReader) ReadPacket() ([]byte, error) {
	time.Sleep(50 * time.Microsecond)
	return []byte{0xAA}, nil
}

func (r *endlessReader) Close() error { r.closed = true; return nil }

// A transport reopen arrives from the playback goroutine while the read-ahead
// producer is running on its own. Every field a reopen writes -- the parser
// index, the position, the retry counts, the cache writer -- is one the
// producer is also reading, so doing the reopen from the caller's goroutine
// raced all of them, including a map the runtime kills the process over.
//
// The point of this test is the race detector, not the assertions.
func TestRequestReopenDoesNotRaceTheReadAheadProducer(t *testing.T) {
	orig := SetRegistry(map[string]parsers.Streamer{
		"p1": fakeStreamer{open: func(*parsers.Track, float64) (opus.Reader, func(), error) {
			r := &endlessReader{}
			return r, func() { _ = r.Close() }, nil
		}},
	})
	defer SetRegistry(orig)

	SetBufferAhead(200)
	defer SetBufferAhead(0)

	track := &parsers.Track{
		Duration:   time.Hour, // finite, so an early end is recoverable
		SourceInfo: sources.TrackInfo{AvailableParsers: []string{"p1"}},
	}
	rs := NewRecoveryStream(track)
	if _, err := rs.Start(0); err != nil {
		t.Fatalf("Open: %v", err)
	}

	packets := rs.Packets()
	done := make(chan struct{})

	var consumer sync.WaitGroup
	consumer.Add(1)
	go func() {
		defer consumer.Done()
		for {
			select {
			case <-done:
				return
			default:
			}
			if _, err := packets.ReadPacket(); err != nil {
				return
			}
		}
	}()

	// What runPlayback does on every ErrVoiceTransport, as fast as the voice
	// layer could ever produce them.
	for i := 0; i < 50; i++ {
		rs.RequestReopen()
		time.Sleep(time.Millisecond)
	}

	close(done)
	consumer.Wait()
	if err := rs.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// Close has to win against a reopen that has been asked for but not yet
// served, or teardown waits forever on a producer that just opened something
// new to read.
func TestCloseDuringPendingReopenDoesNotHang(t *testing.T) {
	orig := SetRegistry(map[string]parsers.Streamer{
		"p1": fakeStreamer{open: func(*parsers.Track, float64) (opus.Reader, func(), error) {
			r := &endlessReader{}
			return r, func() { _ = r.Close() }, nil
		}},
	})
	defer SetRegistry(orig)

	SetBufferAhead(200)
	defer SetBufferAhead(0)

	track := &parsers.Track{
		Duration:   time.Hour,
		SourceInfo: sources.TrackInfo{AvailableParsers: []string{"p1"}},
	}
	rs := NewRecoveryStream(track)
	if _, err := rs.Start(0); err != nil {
		t.Fatalf("Open: %v", err)
	}
	_ = rs.Packets()

	rs.RequestReopen()

	closed := make(chan error, 1)
	go func() { closed <- rs.Close() }()

	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close hung behind a pending reopen")
	}
}

// A reopen must not spend a parser's recovery budget: the media is fine, the
// voice connection is what failed. Three reopens on a one-parser track have to
// keep playing, where three media failures would have exhausted it.
func TestRepeatedReopensDoNotExhaustParserRecovery(t *testing.T) {
	orig := SetRegistry(map[string]parsers.Streamer{
		"p1": fakeStreamer{open: func(*parsers.Track, float64) (opus.Reader, func(), error) {
			r := &endlessReader{}
			return r, func() { _ = r.Close() }, nil
		}},
	})
	defer SetRegistry(orig)

	track := &parsers.Track{
		Duration:   time.Hour,
		SourceInfo: sources.TrackInfo{AvailableParsers: []string{"p1"}},
	}
	rs := NewRecoveryStream(track)
	defer rs.Close()
	if _, err := rs.Start(0); err != nil {
		t.Fatalf("Open: %v", err)
	}

	for i := 0; i < maxRecoveryAttempts+1; i++ {
		if _, err := rs.ReadPacket(); err != nil {
			t.Fatalf("read before reopen %d: %v", i, err)
		}
		rs.RequestReopen()
		if _, err := rs.ReadPacket(); err != nil {
			t.Fatalf("read after reopen %d: %v", i, err)
		}
	}

	if got := rs.retries["p1"]; got != 0 {
		t.Fatalf("a voice reconnect charged the parser %d recovery attempts", got)
	}
}

// Playback resumes where it left off rather than from the start, or a voice
// blip would replay whatever the listener had already heard.
func TestReopenResumesAtTheCurrentPosition(t *testing.T) {
	var seeks []float64
	orig := SetRegistry(map[string]parsers.Streamer{
		"p1": fakeStreamer{open: func(_ *parsers.Track, seek float64) (opus.Reader, func(), error) {
			seeks = append(seeks, seek)
			r := &endlessReader{}
			return r, func() { _ = r.Close() }, nil
		}},
	})
	defer SetRegistry(orig)

	track := &parsers.Track{
		Duration:   time.Hour,
		SourceInfo: sources.TrackInfo{AvailableParsers: []string{"p1"}},
	}
	rs := NewRecoveryStream(track)
	defer rs.Close()
	if _, err := rs.Start(0); err != nil {
		t.Fatalf("Open: %v", err)
	}

	const played = 100
	for i := 0; i < played; i++ {
		if _, err := rs.ReadPacket(); err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
	}
	rs.RequestReopen()
	if _, err := rs.ReadPacket(); err != nil {
		t.Fatalf("read after reopen: %v", err)
	}

	if len(seeks) != 2 {
		t.Fatalf("want two opens, got %d", len(seeks))
	}
	// The position is a packet count times 20ms, so it accumulates float
	// error; a millisecond is far tighter than "did it resume or restart".
	want := float64(played) * float64(opus.FrameMs) / 1000
	if math.Abs(seeks[1]-want) > 0.001 {
		t.Fatalf("reopened at %v, want the position already played (%v)", seeks[1], want)
	}
}
