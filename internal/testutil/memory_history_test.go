package testutil

import (
	"errors"
	"testing"
	"time"

	appmusic "github.com/keshon/melodix/internal/app/music"
	"github.com/keshon/melodix/internal/domain"
)

func TestMemoryHistoryRepository_GetListAppend(t *testing.T) {
	t.Parallel()
	r := NewMemoryHistoryRepository(100)
	g := "g1"
	rec := domain.MusicPlaybackAppend{
		URL:              "https://example.com/a",
		Title:            "A",
		CurrentParser:    "p1",
		AvailableParsers: []string{"p1"},
	}
	at := time.Unix(1, 0)
	id, err := r.AppendMusicPlayback(g, at, rec)
	if err != nil || id != 1 {
		t.Fatalf("append id=%d err=%v", id, err)
	}
	got, err := r.GetMusicPlayback(g, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != rec.URL {
		t.Fatalf("get %+v", got)
	}
	ti := appmusic.TrackInfoFromMusicPlayback(got)
	if ti.AvailableParsers[0] != "p1" {
		t.Fatalf("trackinfo %+v", ti)
	}
	list, err := r.ListMusicPlaybackTimeline(g)
	if err != nil || len(list) != 1 {
		t.Fatalf("list %v err=%v", list, err)
	}
}

func TestMemoryHistoryRepository_NotFound(t *testing.T) {
	t.Parallel()
	r := NewMemoryHistoryRepository(10)
	_, err := r.GetMusicPlayback("g", 99)
	if !errors.Is(err, domain.ErrMusicPlaybackNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
}
