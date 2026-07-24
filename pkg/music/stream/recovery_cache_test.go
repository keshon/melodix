package stream

import (
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/keshon/melodix/pkg/music/cache"
	"github.com/keshon/melodix/pkg/music/opus"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/sources"
	"github.com/rs/zerolog"
)

func newTestCacheStore(t *testing.T) *cache.Store {
	t.Helper()
	s, err := cache.New(cache.Config{Dir: filepath.Join(t.TempDir(), "c"), Persistent: false}, nil, zerolog.Nop())
	if err != nil {
		t.Fatalf("cache.New: %v", err)
	}
	return s
}

func ytTrack(url string, parsersList ...string) *parsers.Track {
	return &parsers.Track{
		URL:        url,
		SourceInfo: sources.TrackInfo{SourceName: sources.YouTube, AvailableParsers: parsersList},
	}
}

func writeCacheBlob(t *testing.T, s *cache.Store, key string, pkts ...[]byte) {
	t.Helper()
	w, err := s.NewWriter(key, cache.Meta{Source: "youtube"})
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	for _, p := range pkts {
		if err := w.Write(p); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if err := w.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}

func TestRecovery_CacheHit_ServesBlobAndSkipsParser(t *testing.T) {
	store := newTestCacheStore(t)
	writeCacheBlob(t, store, "youtube:hit1", []byte{0xC0}, []byte{0xC1})
	SetCache(store)
	defer SetCache(nil)

	orig := SetRegistry(map[string]parsers.Streamer{
		"p1": fakeStreamer{open: func(*parsers.Track, float64) (opus.Reader, func(), error) {
			t.Error("parser must not be opened on a cache hit")
			return errFirst{}, func() {}, nil
		}},
	})
	defer SetRegistry(orig)

	track := ytTrack("https://youtu.be/hit1", "p1")
	rs := NewRecoveryStream(track)
	defer rs.Close()
	if err := rs.Open(0); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !track.Cached || rs.Parser() != "" {
		t.Fatalf("cache hit should set Cached and empty parser (Cached=%v parser=%q)", track.Cached, rs.Parser())
	}
	for _, want := range []byte{0xC0, 0xC1} {
		pkt, err := rs.ReadPacket()
		if err != nil || len(pkt) != 1 || pkt[0] != want {
			t.Fatalf("ReadPacket = (%v,%v), want [%#x]", pkt, err, want)
		}
	}
	if _, err := rs.ReadPacket(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF at blob end, got %v", err)
	}
}

func TestRecovery_CacheInstantFail_FallsBackToParser(t *testing.T) {
	store := newTestCacheStore(t)
	writeCacheBlob(t, store, "youtube:empty1") // header only, zero packets → immediate EOF
	SetCache(store)
	defer SetCache(nil)

	orig := SetRegistry(map[string]parsers.Streamer{
		"p1": fakeStreamer{open: func(*parsers.Track, float64) (opus.Reader, func(), error) {
			return &pktReader{pkts: [][]byte{{0xAA}}}, func() {}, nil
		}},
	})
	defer SetRegistry(orig)

	track := ytTrack("https://youtu.be/empty1", "p1")
	rs := NewRecoveryStream(track)
	defer rs.Close()
	if err := rs.Open(0); err != nil {
		t.Fatalf("Open: %v", err)
	}
	pkt, err := rs.ReadPacket() // cache instant-fails → fall back to p1
	if err != nil || len(pkt) != 1 || pkt[0] != 0xAA {
		t.Fatalf("expected fallback to p1's packet, got (%v,%v)", pkt, err)
	}
	if track.Cached || track.CurrentParser != "p1" {
		t.Fatalf("after fallback want Cached=false parser=p1, got Cached=%v parser=%q", track.Cached, track.CurrentParser)
	}
}

func TestRecovery_WriteThrough_CachesCleanPlay(t *testing.T) {
	store := newTestCacheStore(t)
	SetCache(store)
	defer SetCache(nil)

	orig := SetRegistry(map[string]parsers.Streamer{
		"p1": fakeStreamer{open: func(*parsers.Track, float64) (opus.Reader, func(), error) {
			return &pktReader{pkts: [][]byte{{1}, {2}, {3}}}, func() {}, nil
		}},
	})
	defer SetRegistry(orig)

	track := ytTrack("https://youtu.be/writethru1", "p1")
	track.Duration = 60 * time.Millisecond // 3×20ms, so EOF at the end is "natural"
	rs := NewRecoveryStream(track)
	if err := rs.Open(0); err != nil {
		t.Fatalf("Open: %v", err)
	}
	for {
		_, err := rs.ReadPacket()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("ReadPacket: %v", err)
		}
	}

	key, _ := cache.KeyFrom(sources.YouTube, "https://youtu.be/writethru1")
	if !store.Has(key) {
		t.Fatal("clean play should have been cached via write-through")
	}
	r, err := store.OpenAt(key, 0)
	if err != nil {
		t.Fatalf("OpenAt cached blob: %v", err)
	}
	defer r.Close()
	for _, want := range []byte{1, 2, 3} {
		pkt, err := r.ReadPacket()
		if err != nil || len(pkt) != 1 || pkt[0] != want {
			t.Fatalf("cached packet = (%v,%v), want [%d]", pkt, err, want)
		}
	}
	_ = rs.Close()
}

// The write-through blob must span a mid-stream parser switch — the exact
// scenario from the field log (kkdai-link 403 → kkdai-pipe), and the reason the
// write lives above recovery instead of wrapping a single parser stream.
func TestRecovery_WriteThrough_SurvivesParserSwitch(t *testing.T) {
	store := newTestCacheStore(t)
	SetCache(store)
	defer SetCache(nil)

	orig := SetRegistry(map[string]parsers.Streamer{
		"p1": fakeStreamer{open: func(*parsers.Track, float64) (opus.Reader, func(), error) {
			return errFirst{}, func() {}, nil // opens, then fails on first read
		}},
		"p2": fakeStreamer{open: func(*parsers.Track, float64) (opus.Reader, func(), error) {
			return &pktReader{pkts: [][]byte{{7}, {8}, {9}}}, func() {}, nil
		}},
	})
	defer SetRegistry(orig)

	track := ytTrack("https://youtu.be/switch1", "p1", "p2")
	track.Duration = 60 * time.Millisecond
	rs := NewRecoveryStream(track)
	if err := rs.Open(0); err != nil {
		t.Fatalf("Open: %v", err)
	}
	for {
		_, err := rs.ReadPacket()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("ReadPacket: %v", err)
		}
	}

	key, _ := cache.KeyFrom(sources.YouTube, "https://youtu.be/switch1")
	if !store.Has(key) {
		t.Fatal("blob should survive the p1→p2 switch and cache p2's stream")
	}
	r, err := store.OpenAt(key, 0)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	defer r.Close()
	for _, want := range []byte{7, 8, 9} {
		pkt, err := r.ReadPacket()
		if err != nil || len(pkt) != 1 || pkt[0] != want {
			t.Fatalf("cached packet = (%v,%v), want [%d] (p2's stream)", pkt, err, want)
		}
	}
	_ = rs.Close()
}
