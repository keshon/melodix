package player

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/keshon/melodix/pkg/music/opus"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/sink"
	"github.com/keshon/melodix/pkg/music/sources"
	"github.com/keshon/melodix/pkg/music/stream"
)

// fakeStreamer implements parsers.Streamer for tests: swap stream.Registry with
// fakes and restore afterwards.
type fakeStreamer struct {
	open func(track *parsers.Track, seek float64) (opus.Reader, func(), error)
}

func (s fakeStreamer) Open(track *parsers.Track, seek float64) (opus.Reader, func(), error) {
	return s.open(track, seek)
}

func swapRegistry(t *testing.T, reg map[string]parsers.Streamer) {
	t.Helper()
	orig := stream.Registry
	stream.Registry = reg
	t.Cleanup(func() { stream.Registry = orig })
}

// openLog records which tracks were opened, in order (mutex-guarded for the hammer test).
type openLog struct {
	mu     sync.Mutex
	titles []string
}

func (l *openLog) add(title string) {
	l.mu.Lock()
	l.titles = append(l.titles, title)
	l.mu.Unlock()
}

func (l *openLog) list() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.titles...)
}

// okStreamer serves a short Opus burst (3 real 20ms packets) and fills in a tiny
// track.Duration so RecoveryStream treats the end as natural.
func okStreamer(opened *openLog) fakeStreamer {
	return fakeStreamer{
		open: func(track *parsers.Track, seek float64) (opus.Reader, func(), error) {
			track.Duration = time.Millisecond
			if opened != nil {
				opened.add(track.Title)
			}
			pcm := make([]byte, opus.PCMFrameBytes*3)
			return opus.Encode(io.NopCloser(bytes.NewReader(pcm))), func() {}, nil
		},
	}
}

func badStreamer() fakeStreamer {
	return fakeStreamer{
		open: func(track *parsers.Track, seek float64) (opus.Reader, func(), error) {
			return nil, nil, errors.New("open failed")
		},
	}
}

func testTrack(title string, parserNames ...string) sources.TrackInfo {
	return sources.TrackInfo{
		URL:              "https://example.com/" + title,
		Title:            title,
		SourceName:       "test",
		AvailableParsers: parserNames,
	}
}

// fakeSink drains the stream to EOF ("drain") or blocks until stop closes ("block").
type fakeSink struct {
	block bool
}

func (s *fakeSink) Stream(r opus.Reader, stop <-chan struct{}) error {
	if s.block {
		<-stop
		return stream.ErrPlaybackStopped
	}
	for {
		if _, err := r.ReadPacket(); err != nil {
			return nil
		}
		select {
		case <-stop:
			return stream.ErrPlaybackStopped
		default:
		}
	}
}

type fakeProvider struct {
	mu       sync.Mutex
	sink     sink.AudioSink
	sinkErr  error
	releases int
	// releaseCh receives one signal per ReleaseSink call (buffered; drops when full).
	releaseCh chan struct{}
}

func newFakeProvider(s sink.AudioSink) *fakeProvider {
	return &fakeProvider{sink: s, releaseCh: make(chan struct{}, 8)}
}

func (p *fakeProvider) Sink(target string) (sink.AudioSink, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sinkErr != nil {
		return nil, p.sinkErr
	}
	return p.sink, nil
}

func (p *fakeProvider) ReleaseSink(target string) {
	p.mu.Lock()
	p.releases++
	p.mu.Unlock()
	select {
	case p.releaseCh <- struct{}{}:
	default:
	}
}

func (p *fakeProvider) InvalidateSink() {}

func (p *fakeProvider) releaseCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.releases
}

type fakeResolver struct {
	tracks []sources.TrackInfo
	err    error
}

func (r fakeResolver) Resolve(input, source, parser string) ([]sources.TrackInfo, error) {
	return r.tracks, r.err
}

func waitRelease(t *testing.T, p *fakeProvider, timeout time.Duration) {
	t.Helper()
	select {
	case <-p.releaseCh:
	case <-time.After(timeout):
		t.Fatal("timed out waiting for ReleaseSink")
	}
}

func TestPlayAndAutoAdvance(t *testing.T) {
	opened := &openLog{}
	swapRegistry(t, map[string]parsers.Streamer{"ok": okStreamer(opened)})
	provider := newFakeProvider(&fakeSink{})
	p := New(provider, fakeResolver{})

	// Count Playing statuses live: one per started track proves auto-advance reached track 2.
	var playingMu sync.Mutex
	playing := 0
	stopCollect := make(chan struct{})
	go func() {
		for {
			select {
			case <-stopCollect:
				return
			case s := <-p.PlayerStatus:
				if s == StatusPlaying {
					playingMu.Lock()
					playing++
					playingMu.Unlock()
				}
			}
		}
	}()

	if err := p.EnqueueTrackInfo(testTrack("one", "ok")); err != nil {
		t.Fatalf("enqueue one: %v", err)
	}
	if err := p.EnqueueTrackInfo(testTrack("two", "ok")); err != nil {
		t.Fatalf("enqueue two: %v", err)
	}
	if err := p.PlayNext(""); err != nil {
		t.Fatalf("PlayNext: %v", err)
	}

	// Queue exhaustion ends with Stop(true) -> ReleaseSink.
	waitRelease(t, provider, 5*time.Second)
	time.Sleep(200 * time.Millisecond) // let any duplicate stop path fire before counting
	close(stopCollect)

	if got := opened.list(); len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("expected tracks [one two] to open in order, got %v", got)
	}
	playingMu.Lock()
	gotPlaying := playing
	playingMu.Unlock()
	if gotPlaying < 2 {
		t.Fatalf("expected 2 Playing statuses (auto-advance), got %d", gotPlaying)
	}
	if p.IsPlaying() {
		t.Fatal("player should be idle after queue end")
	}
	if n := provider.releaseCount(); n != 1 {
		t.Fatalf("expected exactly 1 ReleaseSink on natural queue end, got %d", n)
	}
}

func TestStopUnblocksBlockedSink(t *testing.T) {
	swapRegistry(t, map[string]parsers.Streamer{"ok": okStreamer(nil)})
	provider := newFakeProvider(&fakeSink{block: true})
	p := New(provider, fakeResolver{})

	if err := p.EnqueueTrackInfo(testTrack("one", "ok")); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := p.PlayNext(""); err != nil {
		t.Fatalf("PlayNext: %v", err)
	}
	if !p.IsPlaying() {
		t.Fatal("expected playing state while sink is blocked")
	}

	start := time.Now()
	if err := p.Stop(false); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("Stop took %v; should unblock the sink promptly", elapsed)
	}
	if p.IsPlaying() {
		t.Fatal("player should not be playing after Stop")
	}
}

func TestPlayNextEmptyQueue(t *testing.T) {
	swapRegistry(t, map[string]parsers.Streamer{})
	p := New(newFakeProvider(&fakeSink{}), fakeResolver{})
	if err := p.PlayNext(""); !errors.Is(err, ErrNoTracksInQueue) {
		t.Fatalf("expected ErrNoTracksInQueue, got %v", err)
	}
}

func TestStartFailureSkipsToNextTrack(t *testing.T) {
	opened := &openLog{}
	swapRegistry(t, map[string]parsers.Streamer{
		"ok":  okStreamer(opened),
		"bad": badStreamer(),
	})
	provider := newFakeProvider(&fakeSink{})
	p := New(provider, fakeResolver{})

	if err := p.EnqueueTrackInfo(testTrack("broken", "bad")); err != nil {
		t.Fatalf("enqueue broken: %v", err)
	}
	if err := p.EnqueueTrackInfo(testTrack("good", "ok")); err != nil {
		t.Fatalf("enqueue good: %v", err)
	}
	if err := p.PlayNext(""); err != nil {
		t.Fatalf("PlayNext should skip to the good track, got: %v", err)
	}
	waitRelease(t, provider, 5*time.Second)
	if got := opened.list(); len(got) != 1 || got[0] != "good" {
		t.Fatalf("expected only [good] to open, got %v", got)
	}
}

func TestAllTracksFailToStart(t *testing.T) {
	swapRegistry(t, map[string]parsers.Streamer{"bad": badStreamer()})
	p := New(newFakeProvider(&fakeSink{}), fakeResolver{})

	if err := p.EnqueueTrackInfo(testTrack("broken", "bad")); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := p.PlayNext(""); !errors.Is(err, ErrTrackStartFailed) {
		t.Fatalf("expected ErrTrackStartFailed, got %v", err)
	}
}

// TestConcurrentHammer races the public API; the assertion is clean completion under -race.
func TestConcurrentHammer(t *testing.T) {
	swapRegistry(t, map[string]parsers.Streamer{"ok": okStreamer(nil)})
	provider := newFakeProvider(&fakeSink{})
	res := fakeResolver{tracks: []sources.TrackInfo{testTrack("hammer", "ok")}}
	p := New(provider, res)

	deadline := time.Now().Add(300 * time.Millisecond)
	var wg sync.WaitGroup
	ops := []func(){
		func() { _ = p.Enqueue("hammer", "", "") },
		func() { _ = p.PlayNext("") },
		func() { _ = p.Stop(false) },
		func() { _ = p.Queue() },
		func() { _ = p.CurrentTrack() },
		func() { _ = p.IsPlaying() },
	}
	for _, op := range ops {
		wg.Add(1)
		go func(f func()) {
			defer wg.Done()
			for time.Now().Before(deadline) {
				f()
			}
		}(op)
	}
	wg.Wait()
	_ = p.Stop(true)
}

func TestOnPlaybackFailedCalled(t *testing.T) {
	swapRegistry(t, map[string]parsers.Streamer{"ok": okStreamer(nil)})
	provider := newFakeProvider(nil)
	provider.sinkErr = errors.New("no voice")

	type failure struct {
		guildID string
		track   parsers.Track
		err     error
	}
	failedCh := make(chan failure, 1)
	p := NewWithOptions(provider, fakeResolver{}, Options{
		OnPlaybackFailed: func(guildID string, track parsers.Track, err error) {
			select {
			case failedCh <- failure{guildID, track, err}:
			default:
			}
		},
	})
	p.SetGuildID("guild-1")

	if err := p.EnqueueTrackInfo(testTrack("doomed", "ok")); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := p.PlayNext(""); err != nil {
		t.Fatalf("PlayNext: %v", err)
	}

	select {
	case f := <-failedCh:
		if f.guildID != "guild-1" {
			t.Fatalf("guildID = %q, want guild-1", f.guildID)
		}
		if f.track.Title != "doomed" {
			t.Fatalf("track title = %q, want doomed", f.track.Title)
		}
		if !errors.Is(f.err, ErrSinkUnavailable) {
			t.Fatalf("err = %v, want ErrSinkUnavailable", f.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OnPlaybackFailed was not called")
	}
}
