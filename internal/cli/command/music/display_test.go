package music

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/keshon/melodix/internal/domain"
	"github.com/keshon/melodix/internal/musicapp"
	"github.com/keshon/melodix/internal/storage"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/sources"
)

func TestFormatTimelineLinePlainShape(t *testing.T) {
	t.Parallel()
	m := domain.MusicPlayback{
		ID:        7,
		PlayedAt:  time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC),
		URL:       "https://x.test/a",
		Title:     "Hi",
	}
	s := FormatTimelineLinePlain(m)
	if !strings.Contains(s, "7") || !strings.Contains(s, "Hi") || !strings.Contains(s, "https://x.test/a") {
		t.Fatalf("got %q", s)
	}
	if strings.Contains(s, "[") || strings.Contains(s, "](") {
		t.Fatalf("unexpected markdown: %q", s)
	}
}

func TestFormatCountsLinePlainShape(t *testing.T) {
	t.Parallel()
	r := domain.PlaybackCountRow{
		RepresentativeID: 9,
		URL:              "https://y.test/b",
		Title:            "Song",
		Count:            4,
		LastPlayed:       time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	s := FormatCountsLinePlain(r)
	if !strings.Contains(s, "9") || !strings.Contains(s, "x4") {
		t.Fatalf("got %q", s)
	}
}

func TestPresentHistoryCLIPlainTitle(t *testing.T) {
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
	page, err := musicapp.New(s).BuildHistoryPage(guild, 1, "timeline")
	if err != nil {
		t.Fatal(err)
	}
	title, _, rows := PresentHistoryCLI(page)
	if title != TitleHistoryTimeline {
		t.Fatalf("title: got %q want %q", title, TitleHistoryTimeline)
	}
	if strings.Contains(title, "🎵") {
		t.Fatalf("CLI title must not use emoji: %q", title)
	}
	if len(rows) != 1 {
		t.Fatalf("rows: got %d want 1", len(rows))
	}
	line := FormatTimelineLinePlain(page.Rows[0])
	if line == "" {
		t.Fatal("empty line")
	}
	if line[0] == '[' || line[0] == '`' {
		t.Fatalf("expected plain text line, got %q", line)
	}
}
