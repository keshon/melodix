package player

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/keshon/melodix/pkg/music/opus"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/sink"
	"github.com/keshon/melodix/pkg/music/sources"
)

// countingSink reports how many runs are inside Stream at once, which is the
// invariant a playback engine has that nothing in its type system states: two
// runs streaming into one voice connection is two tracks fighting over the
// same 20ms clock.
type countingSink struct {
	live atomic.Int64
	peak atomic.Int64
}

func (s *countingSink) Stream(r opus.Reader, stop <-chan struct{}) error {
	live := s.live.Add(1)
	for {
		if peak := s.peak.Load(); live > peak {
			if s.peak.CompareAndSwap(peak, live) {
				break
			}
			continue
		}
		break
	}
	defer s.live.Add(-1)

	for {
		select {
		case <-stop:
			return nil
		default:
		}
		if _, err := r.ReadPacket(); err != nil {
			return nil
		}
	}
}

// blockingProvider's ReleaseSink takes as long as leaving a real voice channel
// can: the gateway is waited on, and the usual reason a channel is being left
// is that it stopped answering.
type blockingProvider struct {
	sink        sink.AudioSink
	releasing   chan struct{}
	unblock     chan struct{}
	releaseOnce sync.Once
}

func (p *blockingProvider) Sink(string) (sink.AudioSink, error) { return p.sink, nil }
func (p *blockingProvider) InvalidateSink()                     {}

func (p *blockingProvider) ReleaseSink(string) {
	p.releaseOnce.Do(func() {
		close(p.releasing)
		<-p.unblock
	})
}

// A stop that is leaving a voice channel must not take the rest of the guild
// with it. Under serial gateway dispatch the caller blocked behind this lock
// was every command in every guild.
func TestSlowReleaseSinkDoesNotBlockTheRestOfThePlayer(t *testing.T) {
	swapRegistry(t, map[string]parsers.Streamer{"plays": pacedStreamer("t", 2000)})

	provider := &blockingProvider{
		sink:      &fakeSink{block: true},
		releasing: make(chan struct{}),
		unblock:   make(chan struct{}),
	}
	p := New(provider, nil)
	if err := p.EnqueueTrackInfos([]sources.TrackInfo{testTrack("t1", "plays")}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := p.PlayNext("chan"); err != nil {
		t.Fatalf("play: %v", err)
	}

	stopped := make(chan struct{})
	go func() { _ = p.Stop(true); close(stopped) }()

	select {
	case <-provider.releasing:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop never reached ReleaseSink")
	}

	// Stop is now parked inside the provider. Everything else must still work.
	answered := make(chan struct{})
	go func() {
		_ = p.Queue()
		_, _ = p.CurrentTrack()
		_ = p.IsPlaying()
		_ = p.ChannelID()
		close(answered)
	}()

	select {
	case <-answered:
	case <-time.After(2 * time.Second):
		t.Fatal("the player held its lock across a voice disconnect")
	}

	close(provider.unblock)
	<-stopped
}

// The interleaving M-04 described: a stop waits for a run that ends naturally,
// the completion chain starts the next track while it waits, and the stop then
// resets state belonging to a run it never asked about.
func TestStopDoesNotResetANewerRun(t *testing.T) {
	swapRegistry(t, map[string]parsers.Streamer{"plays": pacedStreamer("t", 4000)})

	counter := &countingSink{}
	p := New(newFakeProvider(counter), nil)
	tracks := make([]sources.TrackInfo, 0, 8)
	for i := 0; i < 8; i++ {
		tracks = append(tracks, testTrack("t", "plays"))
	}
	if err := p.EnqueueTrackInfos(tracks); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := p.PlayNext(""); err != nil {
		t.Fatalf("play: %v", err)
	}

	// Skip repeatedly, which is what puts a Stop and a natural track end in the
	// same window.
	for i := 0; i < 40; i++ {
		_ = p.Stop(false)
		if err := p.PlayNext(""); err != nil {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}

	_ = p.Stop(true)
	waitFor(t, 5*time.Second, func() bool { return counter.live.Load() == 0 })

	if peak := counter.peak.Load(); peak > 1 {
		t.Fatalf("%d runs streamed into one sink at once", peak)
	}
	if playing, _ := p.CurrentTrack(); playing.Title != "" {
		t.Fatalf("a track is current after Stop(true): %q", playing.Title)
	}
	if p.IsPlaying() {
		t.Fatal("the player reports playing after Stop(true)")
	}
}

// A run that has been superseded must not clear the state of the one that
// superseded it, which is what leaves audio playing that the player believes
// is not.
func TestASupersededRunDoesNotClearTheCurrentOne(t *testing.T) {
	swapRegistry(t, map[string]parsers.Streamer{"plays": pacedStreamer("t", 4000)})

	p := New(newFakeProvider(&fakeSink{block: true}), nil)
	if err := p.EnqueueTrackInfos([]sources.TrackInfo{
		testTrack("first", "plays"),
		testTrack("second", "plays"),
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := p.PlayNext(""); err != nil {
		t.Fatalf("play first: %v", err)
	}

	p.mu.Lock()
	stale := p.gen
	p.mu.Unlock()

	if err := p.PlayNext(""); err != nil {
		t.Fatalf("play second: %v", err)
	}

	// The older run's goroutine, arriving late.
	p.clearIfCurrent(stale)

	if !p.IsPlaying() {
		t.Fatal("a finished run cleared the run that replaced it")
	}
	if track, ok := p.CurrentTrack(); !ok || track.SourceInfo.Title != "second" {
		t.Fatalf("current track is %+v, want the newer run's", track)
	}
	_ = p.Stop(true)
}

var _ parsers.Streamer = fakeStreamer{}

// A /play that lands exactly as the queue runs dry is the commonest way a
// queue stops being empty, and it used to be the way to lose a track: the
// finishing run had already decided the queue was empty, and its teardown
// cleared the queue the new track had just been added to, leaving the voice
// channel it was about to play in.
//
// The teardown now names its own run, so this drives it directly: the
// finishing run's stop, arriving after a newer one has started.
func TestAQueueEndTeardownDoesNotStopTheTrackThatFollowedIt(t *testing.T) {
	swapRegistry(t, map[string]parsers.Streamer{"plays": pacedStreamer("t", 4000)})

	provider := newFakeProvider(&fakeSink{block: true})
	p := New(provider, nil)
	if err := p.EnqueueTrackInfos([]sources.TrackInfo{testTrack("first", "plays")}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := p.PlayNext("chan"); err != nil {
		t.Fatalf("play first: %v", err)
	}
	p.mu.Lock()
	finishing := p.gen
	p.mu.Unlock()

	// What /play does when it arrives at the boundary.
	if err := p.EnqueueTrackInfos([]sources.TrackInfo{testTrack("second", "plays")}); err != nil {
		t.Fatalf("enqueue second: %v", err)
	}
	if err := p.PlayNext("chan"); err != nil {
		t.Fatalf("play second: %v", err)
	}

	// The finishing run's teardown, arriving late.
	_ = p.stop(true, finishing)

	if !p.IsPlaying() {
		t.Fatal("the queue-end teardown stopped the track that replaced it")
	}
	track, ok := p.CurrentTrack()
	if !ok || track.SourceInfo.Title != "second" {
		t.Fatalf("current track is %+v, want the newly queued one", track)
	}
	if got := provider.releaseCount(); got != 0 {
		t.Fatalf("left the voice channel %d times under a track that was playing", got)
	}
	_ = p.Stop(true)
}

// The other half: with nothing to supersede it, the queue-end teardown must
// still happen, or the bot sits in a voice channel with an empty queue.
func TestAQueueEndTeardownStillLeavesWhenNothingFollowed(t *testing.T) {
	swapRegistry(t, map[string]parsers.Streamer{"plays": pacedStreamer("t", 40)})

	provider := newFakeProvider(&fakeSink{})
	p := New(provider, nil)
	if err := p.EnqueueTrackInfos([]sources.TrackInfo{testTrack("only", "plays")}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := p.PlayNext("chan"); err != nil {
		t.Fatalf("play: %v", err)
	}

	waitRelease(t, provider, 5*time.Second)
	if p.IsPlaying() {
		t.Fatal("still playing after the queue ended")
	}
}

// A user asking to stop means whatever is playing, whichever run that is.
func TestStopWithoutAGenerationStopsWhateverIsPlaying(t *testing.T) {
	swapRegistry(t, map[string]parsers.Streamer{"plays": pacedStreamer("t", 4000)})

	provider := newFakeProvider(&fakeSink{block: true})
	p := New(provider, nil)
	if err := p.EnqueueTrackInfos([]sources.TrackInfo{
		testTrack("first", "plays"),
		testTrack("second", "plays"),
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := p.PlayNext("chan"); err != nil {
		t.Fatalf("play first: %v", err)
	}
	if err := p.PlayNext("chan"); err != nil {
		t.Fatalf("play second: %v", err)
	}

	if err := p.Stop(true); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if p.IsPlaying() {
		t.Fatal("/stop left a track playing")
	}
	if len(p.Queue()) != 0 {
		t.Fatal("/stop left the queue populated")
	}
}

// The interleaving TestStopDoesNotResetANewerRun cannot reach.
//
// For a stop to outlive the run it asked about, that run has to end on its own
// while the stop is waiting for it: only then does the completion chain start
// a replacement, and only then does the stop wake to find a different run in
// charge. Its sibling above plays four-second tracks, so no track ever ends
// inside the skips it does, the completion chain never fires, and only the
// first half of stop's generation check is exercised.
//
// Tracks of three packets end constantly instead, which puts a natural end
// inside almost every stop. Without the second check the stop clears state
// belonging to a run that is still streaming, and mints channels that run can
// then never be stopped by -- measured at six concurrent runs into one sink,
// against one on the code that checks.
func TestAStopNeverResetsARunItDidNotAskAbout(t *testing.T) {
	swapRegistry(t, map[string]parsers.Streamer{"ends": pacedStreamer("t", 3)})

	counter := &countingSink{}
	p := New(newFakeProvider(counter), nil)

	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		tracks := make([]sources.TrackInfo, 0, 12)
		for i := 0; i < 12; i++ {
			tracks = append(tracks, testTrack("t", "ends"))
		}
		if err := p.EnqueueTrackInfos(tracks); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
		if err := p.PlayNext(""); err != nil {
			t.Fatalf("play: %v", err)
		}
		// Skip far faster than a track can play, so a stop and a track ending
		// are constantly in the same window.
		for i := 0; i < 60; i++ {
			_ = p.Stop(false)
			_ = p.PlayNext("")
		}
		_ = p.Stop(true)
	}

	waitFor(t, 5*time.Second, func() bool { return counter.live.Load() == 0 })

	if peak := counter.peak.Load(); peak > 1 {
		t.Fatalf("%d runs streamed into one sink at once: a stop reset a run it never asked about", peak)
	}
}
