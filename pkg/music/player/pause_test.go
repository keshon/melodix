package player

import (
	"errors"
	"testing"
	"time"

	"github.com/keshon/melodix/pkg/music/opus"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/sink"
	"github.com/keshon/melodix/pkg/music/sources"
	"github.com/keshon/melodix/pkg/music/stream"
)

func TestPauseResumeAndSkipUseTheCurrentRun(t *testing.T) {
	swapRegistry(t, map[string]parsers.Streamer{"ok": okStreamer(nil)})
	p := New(newFakeProvider(&fakeSink{block: true}), fakeResolver{})
	t.Cleanup(func() { _ = p.Stop(true) })
	if !errors.Is(p.Pause(), ErrNoTrackPlaying) || !errors.Is(p.Resume(), ErrNoTrackPlaying) {
		t.Fatal("idle player accepted a pause or resume")
	}
	err := p.EnqueueTrackInfos([]sources.TrackInfo{testTrack("first", "ok")})
	if err != nil {
		t.Fatal(err)
	}
	err = p.PlayNext("voice")
	if err != nil {
		t.Fatal(err)
	}
	first := p.run
	err = p.Pause()
	if err != nil || !p.IsPaused() || !p.IsPlaying() || !first.control.Paused() {
		t.Fatalf("pause did not retain a live run: %v", err)
	}
	if !errors.Is(p.Pause(), ErrAlreadyPaused) {
		t.Fatal("repeated pause was accepted")
	}
	err = p.Resume()
	if err != nil || p.IsPaused() || p.run != first {
		t.Fatalf("resume replaced or failed to resume the run: %v", err)
	}
	if !errors.Is(p.Resume(), ErrNotPaused) {
		t.Fatal("repeated resume was accepted")
	}
	err = p.Pause()
	if err != nil {
		t.Fatal(err)
	}
	err = p.EnqueueTrackInfos([]sources.TrackInfo{testTrack("second", "ok")})
	if err != nil {
		t.Fatal(err)
	}
	err = p.PlayNext("voice")
	if err != nil || p.IsPaused() || p.run == first {
		t.Fatalf("skip inherited the paused state: %v", err)
	}
	select {
	case <-first.done:
	case <-time.After(time.Second):
		t.Fatal("paused run did not stop during skip")
	}
}

type pausedRecoverySink struct {
	controls chan *sink.Control
	fail     chan struct{}
	calls    int
}

func (s *pausedRecoverySink) Stream(_ opus.Reader, control *sink.Control) error {
	s.calls++
	s.controls <- control
	if s.calls == 1 {
		select {
		case <-s.fail:
			return stream.ErrVoiceTransport
		case <-control.Stop:
			return stream.ErrPlaybackStopped
		}
	}
	<-control.Stop
	return stream.ErrPlaybackStopped
}

func TestTransportRecoveryPreservesPause(t *testing.T) {
	swapRegistry(t, map[string]parsers.Streamer{"ok": okStreamer(nil)})
	output := &pausedRecoverySink{controls: make(chan *sink.Control, 2), fail: make(chan struct{})}
	p := New(newFakeProvider(output), nil)
	t.Cleanup(func() { _ = p.Stop(true) })
	err := p.EnqueueTrackInfos([]sources.TrackInfo{testTrack("first", "ok")})
	if err != nil {
		t.Fatal(err)
	}
	err = p.PlayNext("voice")
	if err != nil {
		t.Fatal(err)
	}
	var first *sink.Control
	select {
	case first = <-output.controls:
	case <-time.After(time.Second):
		t.Fatal("first sink did not start")
	}
	err = p.Pause()
	if err != nil {
		t.Fatal(err)
	}
	close(output.fail)
	select {
	case recovered := <-output.controls:
		if recovered != first || !recovered.Paused() || !p.IsPaused() {
			t.Fatal("recovery lost the pause control")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("paused transport did not recover")
	}
}
