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
	"github.com/keshon/melodix/pkg/music/sources"
)

// dyingStreamer opens cleanly and fails on the first read, which is the one
// case Open cannot tell apart from a working parser: it is why recovery
// switches parsers mid-track, and why the Now Playing embed has to be
// corrected afterwards.
func dyingStreamer(title string) fakeStreamer {
	return fakeStreamer{
		open: func(track *parsers.Track, _ float64) (opus.Reader, func(), error) {
			track.Title = title
			track.Duration = time.Millisecond
			return failingReader{}, func() {}, nil
		},
	}
}

type failingReader struct{}

func (failingReader) ReadPacket() ([]byte, error) { return nil, errors.New("dead on arrival") }
func (failingReader) Close() error                { return nil }

// namingStreamer plays, and fills in a title of its own on the way -- the
// parser knowing better than the resolver is the ordinary case, not an edge.
func namingStreamer(title string) fakeStreamer {
	return fakeStreamer{
		open: func(track *parsers.Track, _ float64) (opus.Reader, func(), error) {
			track.Title = title
			track.Duration = time.Millisecond
			track.Passthrough = true
			pcm := make([]byte, opus.PCMFrameBytes*40)
			return opus.Encode(io.NopCloser(bytes.NewReader(pcm))), func() {}, nil
		},
	}
}

// pacedStreamer plays for as long as the test needs it to, so a track does not
// end before the assertion about it runs.
func pacedStreamer(title string, packets int) fakeStreamer {
	return fakeStreamer{
		open: func(track *parsers.Track, _ float64) (opus.Reader, func(), error) {
			track.Title = title
			track.Duration = time.Millisecond
			track.Passthrough = true
			return &pacedReader{left: packets}, func() {}, nil
		},
	}
}

type pacedReader struct{ left int }

func (r *pacedReader) ReadPacket() ([]byte, error) {
	if r.left <= 0 {
		return nil, io.EOF
	}
	r.left--
	time.Sleep(time.Millisecond)
	return make([]byte, 40), nil
}

func (r *pacedReader) Close() error { return nil }

// The status watcher renders whatever CurrentTrack hands it, on its own
// goroutine, while a parser switch is rewriting what is playing. Sharing the
// engine's Track with it was a live race on the default configuration; run
// this one under -race, where it is the whole point.
func TestCurrentTrackDoesNotShareStateWithTheStatusWatcher(t *testing.T) {
	swapRegistry(t, map[string]parsers.Streamer{
		"dies":  dyingStreamer("named by the dead parser"),
		"plays": pacedStreamer("named by the live parser", 2000),
	})

	p := New(newFakeProvider(&fakeSink{}), nil)
	if err := p.EnqueueTrackInfos([]sources.TrackInfo{testTrack("t1", "dies", "plays")}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	stopWatching := make(chan struct{})
	var watcher sync.WaitGroup
	watcher.Add(1)
	go func() {
		defer watcher.Done()
		for {
			select {
			case <-stopWatching:
				return
			default:
			}
			// Exactly what the Discord status watcher does with the result.
			if track, ok := p.CurrentTrack(); ok {
				_ = track.Title
				_ = track.CurrentParser
				_ = track.Cached
				_ = track.Passthrough
				_ = track.SourceInfo.SourceName
			}
		}
	}()

	if err := p.PlayNext(""); err != nil {
		t.Fatalf("play: %v", err)
	}
	waitFor(t, time.Second, func() bool {
		track, ok := p.CurrentTrack()
		return ok && track.CurrentParser == "plays"
	})

	close(stopWatching)
	watcher.Wait()
	_ = p.Stop(true)
}

// The first Now Playing used to name the preference rather than the parser
// that opened, because the player only learned what happened by reading a
// Track something else was writing.
func TestCurrentTrackNamesTheParserThatActuallyOpened(t *testing.T) {
	swapRegistry(t, map[string]parsers.Streamer{
		"broken": badStreamer(),
		"plays":  namingStreamer("named by the live parser"),
	})

	p := New(newFakeProvider(&fakeSink{block: true}), nil)
	if err := p.EnqueueTrackInfos([]sources.TrackInfo{testTrack("t1", "broken", "plays")}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := p.PlayNext(""); err != nil {
		t.Fatalf("play: %v", err)
	}

	track, ok := p.CurrentTrack()
	if !ok {
		t.Fatal("nothing is playing")
	}
	if track.CurrentParser != "plays" {
		t.Fatalf("CurrentParser = %q, want the parser that opened", track.CurrentParser)
	}
	if track.Title != "named by the live parser" || !track.Passthrough {
		t.Fatalf("what the parser filled in never reached the player: %+v", track)
	}
	_ = p.Stop(true)
}

// A caller that mutates what CurrentTrack returned must not be mutating the
// queue's or the run's copy.
func TestCurrentTrackIsNotWritableByItsCaller(t *testing.T) {
	swapRegistry(t, map[string]parsers.Streamer{"plays": namingStreamer("original")})

	p := New(newFakeProvider(&fakeSink{block: true}), nil)
	if err := p.EnqueueTrackInfos([]sources.TrackInfo{testTrack("t1", "plays")}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := p.PlayNext(""); err != nil {
		t.Fatalf("play: %v", err)
	}

	track, ok := p.CurrentTrack()
	if !ok {
		t.Fatal("nothing is playing")
	}
	track.Title = "rewritten by a renderer"
	track.SourceInfo.AvailableParsers[0] = "rewritten"

	again, _ := p.CurrentTrack()
	if again.Title != "original" {
		t.Fatalf("a caller rewrote the engine's title: %q", again.Title)
	}
	if again.SourceInfo.AvailableParsers[0] != "plays" {
		t.Fatalf("a caller rewrote the parser list: %v", again.SourceInfo.AvailableParsers)
	}
	_ = p.Stop(true)
}

func waitFor(t *testing.T, within time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition was never met")
}
