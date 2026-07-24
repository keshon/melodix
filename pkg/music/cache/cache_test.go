package cache

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/keshon/melodix/pkg/music/opus"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/sources"
	"github.com/rs/zerolog"
)

func TestKeyCanonicalization(t *testing.T) {
	const yt = "youtube:dQw4w9WgXcQ"
	cases := []struct {
		source, url, want string
		ok                bool
	}{
		{sources.YouTube, "https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=RD1&index=2", yt, true},
		{sources.YouTube, "https://youtu.be/dQw4w9WgXcQ?t=42", yt, true},
		{sources.YouTube, "https://music.youtube.com/watch?v=dQw4w9WgXcQ", yt, true},
		{sources.YouTube, "https://www.youtube.com/shorts/dQw4w9WgXcQ", yt, true},
		{sources.SoundCloud, "https://soundcloud.com/artist/track?in=x/sets/y", "soundcloud:soundcloud.com/artist/track", true},
		{sources.Radio, "http://stream.example/live", "", false},
		{sources.YouTube, "not a url at all", "", false},
	}
	for _, c := range cases {
		got, ok := KeyFrom(c.source, c.url)
		if ok != c.ok || got != c.want {
			t.Fatalf("KeyFrom(%q,%q) = (%q,%v), want (%q,%v)", c.source, c.url, got, ok, c.want, c.ok)
		}
	}

	// The *parsers.Track wrapper resolves to the same key; nil is uncacheable.
	tr := &parsers.Track{URL: "https://youtu.be/dQw4w9WgXcQ", SourceInfo: sources.TrackInfo{SourceName: sources.YouTube}}
	if k, ok := Key(tr); !ok || k != yt {
		t.Fatalf("Key(track) = (%q,%v), want (%q,true)", k, ok, yt)
	}
	if _, ok := Key(nil); ok {
		t.Fatalf("Key(nil) should be uncacheable")
	}
}

func newStore(t *testing.T, maxBytes int64, idx IndexStore, persistent bool) *Store {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "cache")
	s, err := New(Config{Dir: dir, MaxBytes: maxBytes, Persistent: persistent}, idx, zerolog.Nop())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func writeBlob(t *testing.T, s *Store, key string, pkts [][]byte) {
	t.Helper()
	w, err := s.NewWriter(key, Meta{Source: "youtube", Title: key})
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

func drainAll(t *testing.T, r opus.Reader) [][]byte {
	t.Helper()
	defer r.Close()
	var got [][]byte
	for {
		p, err := r.ReadPacket()
		if errors.Is(err, io.EOF) {
			return got
		}
		if err != nil {
			t.Fatalf("ReadPacket: %v", err)
		}
		got = append(got, p)
	}
}

func TestBlobRoundTripAndSeek(t *testing.T) {
	s := newStore(t, 0, nil, false)
	pkts := [][]byte{{1}, {2, 2}, {3, 3, 3}, {4}}
	writeBlob(t, s, "k", pkts)

	if !s.Has("k") {
		t.Fatal("Has(k) = false after commit")
	}

	r, err := s.OpenAt("k", 0)
	if err != nil {
		t.Fatalf("OpenAt 0: %v", err)
	}
	got := drainAll(t, r)
	if len(got) != len(pkts) {
		t.Fatalf("got %d packets, want %d", len(got), len(pkts))
	}
	for i := range pkts {
		if !bytes.Equal(got[i], pkts[i]) {
			t.Fatalf("packet %d = %v, want %v", i, got[i], pkts[i])
		}
	}

	r2, err := s.OpenAt("k", 2)
	if err != nil {
		t.Fatalf("OpenAt 2: %v", err)
	}
	got2 := drainAll(t, r2)
	if len(got2) != 2 || !bytes.Equal(got2[0], pkts[2]) {
		t.Fatalf("seek=2 got %v, want [{3 3 3} {4}]", got2)
	}

	if _, err := s.OpenAt("k", 100); err == nil {
		t.Fatal("OpenAt past end should error")
	}
	if _, err := s.OpenAt("missing", 0); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("OpenAt(missing) = %v, want ErrNotExist", err)
	}
}

func TestWriterAbort(t *testing.T) {
	s := newStore(t, 0, nil, false)
	w, err := s.NewWriter("k", Meta{})
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	_ = w.Write([]byte{1, 2, 3})
	if err := w.Abort(); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	if s.Has("k") {
		t.Fatal("aborted blob should not be cached")
	}
	entries, _ := os.ReadDir(s.cfg.Dir)
	for _, e := range entries {
		t.Fatalf("dir should be empty after abort, found %q", e.Name())
	}
}

func TestLRUEviction(t *testing.T) {
	big := bytes.Repeat([]byte{0xAB}, 1000)
	// Cap fits one blob but not two → the older one is evicted on the 2nd commit.
	s := newStore(t, 1200, nil, false)

	writeBlob(t, s, "k1", [][]byte{big})
	writeBlob(t, s, "k2", [][]byte{big})

	if s.Has("k1") {
		t.Fatal("k1 should have been evicted (least-recently-accessed)")
	}
	if !s.Has("k2") {
		t.Fatal("k2 should remain")
	}
	// k1's blob file must be gone.
	entries, _ := os.ReadDir(s.cfg.Dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 blob file after eviction, found %d", len(entries))
	}
}

type memIdx struct{ m map[string]Entry }

func (x *memIdx) Load() (map[string]Entry, error) { return x.m, nil }
func (x *memIdx) Save(m map[string]Entry) error {
	cp := make(map[string]Entry, len(m))
	for k, v := range m {
		cp[k] = v
	}
	x.m = cp
	return nil
}

func TestPersistenceAndReload(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	idx := &memIdx{}

	s1, err := New(Config{Dir: dir, Persistent: true}, idx, zerolog.Nop())
	if err != nil {
		t.Fatalf("New s1: %v", err)
	}
	writeBlob(t, s1, "k1", [][]byte{{1}, {2}})

	// Reload with the same dir + index → entry survives.
	s2, err := New(Config{Dir: dir, Persistent: true}, idx, zerolog.Nop())
	if err != nil {
		t.Fatalf("New s2: %v", err)
	}
	if !s2.Has("k1") {
		t.Fatal("persistent reload lost k1")
	}

	// A non-persistent store wipes the dir on startup.
	s3, err := New(Config{Dir: dir, Persistent: false}, idx, zerolog.Nop())
	if err != nil {
		t.Fatalf("New s3: %v", err)
	}
	if s3.Has("k1") {
		t.Fatal("non-persistent store should have wiped the cache")
	}
	if _, err := os.Stat(s1.pathFor("k1")); !os.IsNotExist(err) {
		t.Fatal("non-persistent startup should have removed the blob file")
	}
}
