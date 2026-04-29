package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/sources"
	"github.com/rs/zerolog"
)

// Manual verification (Discord):
// - Play the same URL twice; timeline shows two lines with different ids and times; counts shows one row with count 2.
// - Replay from counts row uses the representative id after bot restart; ids and list survive restart.
// - Trim: with many plays, oldest ids return ErrMusicPlaybackNotFound on replay.

func TestAppendGetListMusicPlayback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ds.json")
	ctx := context.Background()
	s, err := NewStorage(ctx, path, zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	// Intentionally omit s.Close(): datastore Close can block on autosave wait in tests.

	guild := "guild1"
	tp := parsers.TrackParse{
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

	dir := t.TempDir()
	path := filepath.Join(dir, "ds.json")
	ctx := context.Background()
	s, err := NewStorage(ctx, path, zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}

	guild := "g2"
	base := parsers.TrackParse{
		URL:           "https://example.com/x",
		Title:         "t",
		CurrentParser: "p",
		SourceInfo: sources.TrackInfo{
			AvailableParsers: []string{"p"},
		},
	}
	for i := 0; i < 4; i++ {
		_, err := s.AppendMusicPlayback(guild, base, time.Unix(int64(i), 0))
		if err != nil {
			t.Fatal(err)
		}
	}

	_, err = s.MusicPlayback(guild, 1)
	if !errors.Is(err, ErrMusicPlaybackNotFound) {
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
