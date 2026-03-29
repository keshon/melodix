package music

import (
	"errors"
	"path/filepath"
	"strings"
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
	_, err = BuildHistoryPage(s, "guild-x", 1, "timeline", HistoryFooterReplayDiscord, formatTimelineLine, formatCountsLine, HistoryTitleTimelineDiscord, HistoryTitleCountsDiscord, false)
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

	p1, err := BuildHistoryPage(s, guild, 1, "timeline", HistoryFooterReplayDiscord, formatTimelineLine, formatCountsLine, HistoryTitleTimelineDiscord, HistoryTitleCountsDiscord, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(p1.Lines) != HistoryLinesPerPage {
		t.Fatalf("page 1 lines: got %d want %d", len(p1.Lines), HistoryLinesPerPage)
	}
	if p1.TotalPages != 2 || p1.TotalRows != n {
		t.Fatalf("meta: TotalPages=%d TotalRows=%d", p1.TotalPages, p1.TotalRows)
	}

	p2, err := BuildHistoryPage(s, guild, 2, "timeline", HistoryFooterReplayDiscord, formatTimelineLine, formatCountsLine, HistoryTitleTimelineDiscord, HistoryTitleCountsDiscord, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(p2.Lines) != 1 {
		t.Fatalf("page 2 lines: got %d want 1", len(p2.Lines))
	}
	if p2.Page != 2 {
		t.Fatalf("Page: got %d want 2", p2.Page)
	}
}

func TestHistoryPageForCLIUsesPlainFormatters(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "ds.json")
	s, err := storage.New(path)
	if err != nil {
		t.Fatal(err)
	}
	guild := "guild-cli"
	tp := parsers.TrackParse{
		URL:           "https://example.com/a",
		Title:         "Song",
		CurrentParser: "p1",
		SourceInfo: sources.TrackInfo{
			URL:              "https://example.com/a",
			Title:            "Song",
			SourceName:       "youtube",
			AvailableParsers: []string{"p1"},
		},
	}
	if _, err := s.AppendMusicPlayback(guild, tp, time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	res, err := HistoryPageForCLI(s, guild, 1, "timeline")
	if err != nil {
		t.Fatal(err)
	}
	if res.Title != HistoryTitleTimelineCLI {
		t.Fatalf("title: got %q want %q", res.Title, HistoryTitleTimelineCLI)
	}
	if strings.Contains(res.Title, "🎵") {
		t.Fatalf("CLI title must not use emoji: %q", res.Title)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("rows: got %d want 1", len(res.Rows))
	}
	if len(res.Lines) != 1 {
		t.Fatalf("lines: %v", res.Lines)
	}
	line := res.Lines[0]
	if line == "" {
		t.Fatal("empty line")
	}
	// Plain format: no markdown link brackets
	if line[0] == '[' || line[0] == '`' {
		t.Fatalf("expected plain text line, got %q", line)
	}
}
