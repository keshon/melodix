package history

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/keshon/melodix/internal/storage"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/sources"
)

func TestBuildHistoryPageEmpty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "ds.json")
	s, err := storage.New(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = BuildHistoryPage(s, "guild-x", 1, "timeline")
	if !errors.Is(err, ErrHistoryEmpty) {
		t.Fatalf("want ErrHistoryEmpty, got %v", err)
	}
}

func TestBuildHistoryPagePagination(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "ds.json")
	s, err := storage.New(path)
	if err != nil {
		t.Fatal(err)
	}
	guild := "guild-paginate"
	n := HistoryLinesPerPage + 1
	for i := 0; i < n; i++ {
		tp := parsers.TrackParse{
			URL:           "https://example.com/t",
			Title:         "Track",
			CurrentParser: "p1",
			SourceInfo: sources.TrackInfo{
				URL:              "https://example.com/t",
				Title:            "Track",
				SourceName:       "youtube",
				AvailableParsers: []string{"p1"},
			},
		}
		if _, err := s.AppendMusicPlayback(guild, tp, time.Unix(int64(i), 0)); err != nil {
			t.Fatal(err)
		}
	}

	p1, err := BuildHistoryPage(s, guild, 1, "timeline")
	if err != nil {
		t.Fatal(err)
	}
	if len(p1.Rows) != HistoryLinesPerPage {
		t.Fatalf("page 1 rows: got %d want %d", len(p1.Rows), HistoryLinesPerPage)
	}
	if p1.TotalPages != 2 || p1.TotalRows != n {
		t.Fatalf("meta: TotalPages=%d TotalRows=%d", p1.TotalPages, p1.TotalRows)
	}

	p2, err := BuildHistoryPage(s, guild, 2, "timeline")
	if err != nil {
		t.Fatal(err)
	}
	if len(p2.Rows) != 1 {
		t.Fatalf("page 2 rows: got %d want 1", len(p2.Rows))
	}
	if p2.Page != 2 {
		t.Fatalf("Page: got %d want 2", p2.Page)
	}
}
