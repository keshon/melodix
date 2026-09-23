package music

import (
	"errors"
	"testing"
	"time"

	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/storage"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/sink"
	"github.com/rs/zerolog"
)

func TestPlayCounts(t *testing.T) {
	t.Parallel()
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	h := []storage.PlaybackEntry{
		{ID: 1, PlayedAt: t1, URL: "https://a.com", Title: "A"},
		{ID: 2, PlayedAt: t2, URL: "https://a.com", Title: "A newer"},
		{ID: 3, PlayedAt: t1, URL: "https://b.com", Title: "B"},
	}
	rows := PlayCounts(h)
	if len(rows) != 2 {
		t.Fatalf("want 2 groups, got %d", len(rows))
	}
	// Sorted by count desc: a.com has 2, b.com has 1
	if rows[0].URL != "https://a.com" || rows[0].Count != 2 || rows[0].ID != 2 || rows[0].Title != "A newer" {
		t.Fatalf("first row: %+v", rows[0])
	}
	if rows[1].Count != 1 || rows[1].ID != 3 {
		t.Fatalf("second row: %+v", rows[1])
	}
}

// Page 1 is what was just played. Storage returns rows oldest first, and the
// timeline used to page them in that order, so a server with a few hundred
// plays opened /history on tracks from weeks ago -- under a command described
// as "Show recently played tracks".
func TestHistoryListsNewestFirst(t *testing.T) {
	t.Parallel()
	store, err := storage.NewStorage(t.TempDir(), zerolog.Nop())
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	day := func(d int) time.Time { return time.Date(2026, 9, d, 0, 0, 0, 0, time.UTC) }
	for i, title := range []string{"oldest", "middle", "newest"} {
		track := parsers.Track{URL: "https://example.com/" + title, Title: title}
		if _, err := store.AppendMusicPlayback("g1", track, day(i+1)); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	s := New(&config.Config{}, store, zerolog.Nop(), Hooks{})
	rows, err := s.History("g1")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	for i, want := range []string{"newest", "middle", "oldest"} {
		if rows[i].Title != want {
			t.Errorf("row %d = %q, want the %s track", i, rows[i].Title, want)
		}
	}
}

func TestHistoryWithoutStorage(t *testing.T) {
	t.Parallel()
	s := New(&config.Config{}, nil, zerolog.Nop(), Hooks{})
	if _, err := s.History("cli"); !errors.Is(err, ErrHistoryUnavailable) {
		t.Fatalf("err = %v, want ErrHistoryUnavailable", err)
	}
	in := Input{Kind: InputHistoryIDs, HistoryIDs: []uint64{1}}
	if _, err := s.Add("cli", in, "", ""); !errors.Is(err, ErrHistoryUnavailable) {
		t.Fatalf("Add err = %v, want ErrHistoryUnavailable", err)
	}
}

func TestSearchRefusesRadio(t *testing.T) {
	t.Parallel()
	s := New(&config.Config{}, nil, zerolog.Nop(), Hooks{})
	if _, err := s.Search("radio", "anything", 5); !errors.Is(err, ErrNotSearchable) {
		t.Fatalf("err = %v, want ErrNotSearchable", err)
	}
	if _, err := s.HitURL("bandcamp", "123"); !errors.Is(err, ErrNotSearchable) {
		t.Fatalf("HitURL err = %v, want ErrNotSearchable", err)
	}
}

func TestHitURLRebuildsYouTubeOffline(t *testing.T) {
	t.Parallel()
	s := New(&config.Config{}, nil, zerolog.Nop(), Hooks{})
	// YouTube rebuilds offline; reaching the network here would be a bug.
	url, err := s.HitURL("youtube", "K0HSD_i2DvA")
	if err != nil || url != "https://www.youtube.com/watch?v=K0HSD_i2DvA" {
		t.Fatalf("url = %q, err = %v", url, err)
	}
}

type noSink struct{}

func (noSink) Sink(string) (sink.AudioSink, error) { return nil, errors.New("no audio in tests") }
func (noSink) ReleaseSink(string)                  {}
func (noSink) InvalidateSink()                     {}

// A history batch is resolved whole before any of it is queued: one bad id
// queues nothing, rather than the ids that happened to come before it.
func TestAddQueuesAHistoryBatchWholeOrNotAtAll(t *testing.T) {
	t.Parallel()
	store, err := storage.NewStorage(t.TempDir(), zerolog.Nop())
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	var ids []uint64
	for _, title := range []string{"one", "two"} {
		track := parsers.Track{URL: "https://example.com/" + title, Title: title, CurrentParser: "ytnative-link"}
		id, err := store.AppendMusicPlayback("g1", track, time.Now())
		if err != nil {
			t.Fatalf("append: %v", err)
		}
		ids = append(ids, id)
	}
	s := New(&config.Config{}, store, zerolog.Nop(), Hooks{
		NewSink: func(string) sink.Provider { return noSink{} },
	})

	bad := Input{Kind: InputHistoryIDs, HistoryIDs: []uint64{ids[0], 999}}
	if _, err := s.Add("g1", bad, "", ""); !errors.Is(err, ErrUnknownHistoryID) {
		t.Fatalf("err = %v, want ErrUnknownHistoryID", err)
	}
	if q := s.Player("g1").Queue(); len(q) != 0 {
		t.Fatalf("queued %d tracks from a batch with a bad id", len(q))
	}

	added, err := s.Add("g1", Input{Kind: InputHistoryIDs, HistoryIDs: ids}, "", "")
	if err != nil || added != 2 {
		t.Fatalf("Add = %d, %v; want 2 queued", added, err)
	}
	if q := s.Player("g1").Queue(); len(q) != 2 || q[0].Title != "one" {
		t.Fatalf("queue = %+v", q)
	}
}
