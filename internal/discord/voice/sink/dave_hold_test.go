package sink

import (
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/keshon/melodix/pkg/music/stream"
)

// fakeGate is a DAVE session's hold decision with nothing else attached, so a
// test can put the send path in each of the states dave-go reports without
// standing up an MLS group.
type fakeGate struct{ hold atomic.Bool }

func (g *fakeGate) ShouldHoldFrames() bool { return g.hold.Load() }

// fakeReader yields identical audible packets forever, counting the reads.
// The count is the assertion that matters: a held frame must not consume a
// packet, or the hold is heard as a gap rather than as a pause.
type fakeReader struct{ reads atomic.Int64 }

func (r *fakeReader) ReadPacket() ([]byte, error) {
	r.reads.Add(1)
	return make([]byte, silenceBytes+1), nil
}

func (r *fakeReader) Close() error { return nil }

// fakeClock is a manually advanced clock, so the hold budget is tested
// exactly rather than by sleeping for it.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time      { return c.t }
func (c *fakeClock) add(d time.Duration) { c.t = c.t.Add(d) }

func newTestProvider(gate daveGate, r *fakeReader, clock *fakeClock) *frameProvider {
	return &frameProvider{
		r:          r,
		stop:       make(chan struct{}),
		dave:       gate,
		holdBudget: daveReadyTimeout,
		now:        clock.now,
		done:       make(chan error, 1),
	}
}

// audible drains the provider until it yields a frame with audio in it, so a
// test asserting "this sends" is not fooled by the warm-up returning nil.
func audible(t *testing.T, p *frameProvider, tries int) []byte {
	t.Helper()
	for i := 0; i < tries; i++ {
		frame, err := p.ProvideOpusFrame()
		if err != nil {
			t.Fatalf("frame %d: unexpected error %v", i, err)
		}
		if len(frame) > 0 {
			return frame
		}
	}
	return nil
}

func TestHoldAllowsAChannelWithoutEncryption(t *testing.T) {
	r := &fakeReader{}
	p := newTestProvider(nil, r, &fakeClock{t: time.Now()})

	if frame := audible(t, p, 4); frame == nil {
		t.Fatal("a channel with no DAVE session must send, not hold")
	}
	if r.reads.Load() == 0 {
		t.Fatal("no packet was read, so nothing was sent")
	}
}

func TestHoldStopsSendingWithoutAnEpoch(t *testing.T) {
	r := &fakeReader{}
	gate := &fakeGate{}
	gate.hold.Store(true)
	p := newTestProvider(gate, r, &fakeClock{t: time.Now()})

	for i := 0; i < 10; i++ {
		frame, err := p.ProvideOpusFrame()
		if err != nil {
			t.Fatalf("frame %d: holding must not end the track: %v", i, err)
		}
		if len(frame) != 0 {
			t.Fatalf("frame %d: a frame went out with no epoch to protect it", i)
		}
	}
	if got := r.reads.Load(); got != 0 {
		t.Fatalf("a held frame consumed %d packets; the hold must not skip audio", got)
	}
	select {
	case err := <-p.done:
		t.Fatalf("holding ended the track early: %v", err)
	default:
	}
}

func TestHoldSendsOnceAnEpochIsLive(t *testing.T) {
	r := &fakeReader{}
	gate := &fakeGate{}
	gate.hold.Store(true)
	clock := &fakeClock{t: time.Now()}
	p := newTestProvider(gate, r, clock)

	for i := 0; i < 5; i++ {
		clock.add(20 * time.Millisecond)
		if frame, _ := p.ProvideOpusFrame(); len(frame) != 0 {
			t.Fatal("sent while holding")
		}
	}

	gate.hold.Store(false)
	if frame := audible(t, p, 4); frame == nil {
		t.Fatal("playback did not resume once the epoch came up")
	}
}

func TestHoldThatNeverResolvesEndsTheTrack(t *testing.T) {
	r := &fakeReader{}
	gate := &fakeGate{}
	gate.hold.Store(true)
	clock := &fakeClock{t: time.Now()}
	p := newTestProvider(gate, r, clock)

	if frame, err := p.ProvideOpusFrame(); err != nil || len(frame) != 0 {
		t.Fatalf("first held frame: frame=%d err=%v", len(frame), err)
	}
	clock.add(daveReadyTimeout + time.Second)

	frame, err := p.ProvideOpusFrame()
	if len(frame) != 0 {
		t.Fatal("an expired hold sent the frame it was holding")
	}
	if !errors.Is(err, io.EOF) {
		t.Fatalf("want io.EOF to stop disgo's sender, got %v", err)
	}

	select {
	case got := <-p.done:
		if !errors.Is(got, stream.ErrVoiceTransport) {
			t.Fatalf("want ErrVoiceTransport, got %v", got)
		}
	default:
		t.Fatal("a hold nobody ended left the track running: this is the wedge")
	}
}

// A re-key mid-track is the ordinary case: the hold has to be able to start
// over, or the first one would spend the budget for every later one.
func TestHoldClockRestartsAfterEachEpoch(t *testing.T) {
	r := &fakeReader{}
	gate := &fakeGate{}
	clock := &fakeClock{t: time.Now()}
	p := newTestProvider(gate, r, clock)

	if frame := audible(t, p, 4); frame == nil {
		t.Fatal("did not start playing")
	}

	gate.hold.Store(true)
	if _, err := p.ProvideOpusFrame(); err != nil {
		t.Fatalf("re-key hold: %v", err)
	}
	clock.add(daveReadyTimeout - time.Second)
	gate.hold.Store(false)
	if _, err := p.ProvideOpusFrame(); err != nil {
		t.Fatalf("resume after re-key: %v", err)
	}

	gate.hold.Store(true)
	if _, err := p.ProvideOpusFrame(); err != nil {
		t.Fatalf("second hold: %v", err)
	}
	clock.add(daveReadyTimeout - time.Second)
	if _, err := p.ProvideOpusFrame(); err != nil {
		t.Fatalf("the second hold inherited the first hold's clock: %v", err)
	}
}
