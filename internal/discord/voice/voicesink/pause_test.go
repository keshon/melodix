package voicesink

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/keshon/melodix/pkg/music/sink"
	"github.com/keshon/melodix/pkg/music/stream"
)

func TestPauseHoldsPacketsAndResumesWithoutPrimingAgain(t *testing.T) {
	audio := &fakeReader{}
	clock := &fakeClock{t: time.Now()}
	p := newTestProvider(nil, audio, clock)
	stop := make(chan struct{})
	p.stop = stop
	p.control = &sink.Control{Stop: stop}
	audible(t, p, 1)
	before := audio.reads.Load()

	p.control.Pause()
	for range 20 {
		frame, err := p.ProvideOpusFrame()
		if len(frame) != 0 || err != nil {
			t.Fatalf("paused pull: frame=%v, err=%v", frame, err)
		}
	}
	if audio.reads.Load() != before {
		t.Fatal("pause consumed track packets")
	}

	p.control.Resume()
	audible(t, p, 1)
	if audio.reads.Load() != before+1 {
		t.Fatal("resume repeated warm-up or skipped packets")
	}

	p.control.Pause()
	close(stop)
	_, err := p.ProvideOpusFrame()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("stopping paused provider: %v", err)
	}
	if err = <-p.done; !errors.Is(err, stream.ErrPlaybackStopped) {
		t.Fatalf("paused stop result: %v", err)
	}
}

func TestLongPauseKeepsVoiceAliveAndStopResponsive(t *testing.T) {
	conn := newStubConn()
	manager := &stubManager{}
	manager.set(conn)
	audio := &endlessAudio{}
	stop := make(chan struct{})
	control := &sink.Control{Stop: stop}
	done := make(chan error, 1)
	go func() { done <- newTestSink(conn, manager).Stream(audio, control) }()

	waitUntil(t, 2*time.Second, func() bool { return conn.udp.writes.Load() > 2 })
	control.Pause()
	// Let the pull already in flight finish before measuring the hold.
	time.Sleep(60 * time.Millisecond)
	before := audio.reads.Load()
	time.Sleep(3 * transportSilence)
	if audio.reads.Load() != before {
		t.Fatal("voice sender consumed packets while paused")
	}
	select {
	case err := <-done:
		t.Fatalf("watchdog ended an intentional pause: %v", err)
	default:
	}

	close(stop)
	select {
	case err := <-done:
		if !errors.Is(err, stream.ErrPlaybackStopped) {
			t.Fatalf("stopping paused track: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("stop blocked on pause")
	}
}
