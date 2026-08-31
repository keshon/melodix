package storage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/keshon/datastore"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/sources"
	"github.com/rs/zerolog"
)

// Manual verification (Discord):
//   - Play the same URL twice; the timeline shows two lines with different
//     ids and times, while counts shows one row with count 2.
//   - Replay from a counts row uses the representative id after a restart;
//     ids and list survive it.
//   - Trim: with many plays, the oldest ids return ErrMusicPlaybackNotFound
//     on replay.

func newTestStorage(t *testing.T) *Storage {
	t.Helper()
	s, err := NewStorage(t.TempDir(), zerolog.Nop())
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestAppendGetListMusicPlayback(t *testing.T) {
	s := newTestStorage(t)

	guild := "guild1"
	tp := parsers.Track{
		URL:           "https://example.com/a",
		Title:         "Song A",
		CurrentParser: "p1",
		SourceInfo: sources.TrackInfo{
			URL:              "https://example.com/a",
			Title:            "Song A",
			SourceName:       "youtube",
			AvailableParsers: []string{"p1", "p2"},
		},
	}
	at := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	id, err := s.AppendMusicPlayback(guild, tp, at)
	if err != nil || id != 1 {
		t.Fatalf("append: id=%d err=%v", id, err)
	}

	got, err := s.MusicPlayback(guild, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 1 || got.URL != tp.URL || got.Title != tp.Title || got.CurrentParser != tp.CurrentParser {
		t.Fatalf("get: %+v", got)
	}
	if len(got.AvailableParsers) != 2 {
		t.Fatalf("available parsers: %v", got.AvailableParsers)
	}

	ti := TrackInfoFromMusicPlayback(got)
	if ti.URL != got.URL || ti.AvailableParsers[0] != "p1" {
		t.Fatalf("trackinfo: %+v", ti)
	}

	list, err := s.ListMusicPlaybackTimeline(guild)
	if err != nil || len(list) != 1 || list[0].ID != 1 {
		t.Fatalf("list: %v err=%v", list, err)
	}
}

func TestMusicPlaybackTrimKeepsRecent(t *testing.T) {
	oldLim := musicPlaybackHistoryLimit
	musicPlaybackHistoryLimit = 3
	t.Cleanup(func() { musicPlaybackHistoryLimit = oldLim })

	s := newTestStorage(t)

	guild := "g2"
	base := parsers.Track{
		URL:           "https://example.com/x",
		Title:         "t",
		CurrentParser: "p",
		SourceInfo: sources.TrackInfo{
			AvailableParsers: []string{"p"},
		},
	}
	for i := 0; i < 4; i++ {
		if _, err := s.AppendMusicPlayback(guild, base, time.Unix(int64(i), 0)); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := s.MusicPlayback(guild, 1); !errors.Is(err, ErrMusicPlaybackNotFound) {
		t.Fatalf("want trimmed id 1 missing, got err=%v", err)
	}
	if _, err := s.MusicPlayback(guild, 4); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListMusicPlaybackTimeline(guild)
	if err != nil || len(list) != 3 {
		t.Fatalf("list len: %d err=%v", len(list), err)
	}
}

// Ids are per guild and rows never leak across guilds.
func TestMusicPlaybackIsolatedPerGuild(t *testing.T) {
	s := newTestStorage(t)
	tr := parsers.Track{URL: "u", Title: "t", SourceInfo: sources.TrackInfo{AvailableParsers: []string{"p"}}}

	idA, err := s.AppendMusicPlayback("gA", tr, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	idB, err := s.AppendMusicPlayback("gB", tr, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	if idA != 1 || idB != 1 {
		t.Fatalf("per-guild ids should both start at 1, got %d and %d", idA, idB)
	}
	if list, _ := s.ListMusicPlaybackTimeline("gA"); len(list) != 1 {
		t.Fatalf("guild A should have exactly its own row, got %d", len(list))
	}
	if _, err := s.MusicPlayback("gA", 99); !errors.Is(err, ErrMusicPlaybackNotFound) {
		t.Fatalf("unknown id: %v", err)
	}
}

// Timeline must come back oldest-first even past 10 rows, where naive string
// ordering of the numeric id would put "10" before "9".
func TestTimelineOrderingPastTenRows(t *testing.T) {
	s := newTestStorage(t)
	tr := parsers.Track{URL: "u", SourceInfo: sources.TrackInfo{AvailableParsers: []string{"p"}}}
	for i := 0; i < 12; i++ {
		if _, err := s.AppendMusicPlayback("g", tr, time.Unix(int64(i), 0)); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.ListMusicPlaybackTimeline("g")
	if err != nil || len(list) != 12 {
		t.Fatalf("list len %d err %v", len(list), err)
	}
	for i, row := range list {
		if row.ID != uint64(i+1) {
			t.Fatalf("row %d has id %d; timeline is not chronological: %v", i, row.ID, list)
		}
	}
}

// The data directory is exclusive to one process. The CLI relies on this
// surfacing as datastore.ErrLocked so it can fall back instead of dying.
func TestSecondOpenIsLocked(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStorage(dir, zerolog.Nop())
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if _, err := NewStorage(dir, zerolog.Nop()); !errors.Is(err, datastore.ErrLocked) {
		t.Fatalf("second open err = %v, want datastore.ErrLocked", err)
	}
}

// The store owns a directory, not a file: it creates the dir and its log there.
func TestStorageCreatesDirectoryLayout(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "store")
	s, err := NewStorage(dir, zerolog.Nop())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if _, err := os.Stat(filepath.Join(dir, "LOCK")); err != nil {
		t.Fatalf("expected a LOCK file in the data dir: %v", err)
	}
}

// State must survive a close/reopen of the same directory.
func TestStoragePersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStorage(dir, zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	tr := parsers.Track{URL: "u", Title: "keep me", SourceInfo: sources.TrackInfo{AvailableParsers: []string{"p"}}}
	if _, err := s.AppendMusicPlayback("g", tr, time.Unix(5, 0)); err != nil {
		t.Fatal(err)
	}
	if err := s.DisableGroup("g", "music"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	s2, err := NewStorage(dir, zerolog.Nop())
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = s2.Close() }()

	row, err := s2.MusicPlayback("g", 1)
	if err != nil || row.Title != "keep me" {
		t.Fatalf("row after reopen: %+v err=%v", row, err)
	}
	// The id counter must continue, not restart.
	id, err := s2.AppendMusicPlayback("g", tr, time.Unix(6, 0))
	if err != nil || id != 2 {
		t.Fatalf("id after reopen = %d err=%v, want 2", id, err)
	}
	if disabled, err := s2.IsGroupDisabled("g", "music"); err != nil || !disabled {
		t.Fatalf("disabled group lost across reopen (%v, %v)", disabled, err)
	}
}
