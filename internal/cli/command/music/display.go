package music

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/keshon/melodix/internal/domain"
	"github.com/keshon/melodix/internal/history"
	"github.com/keshon/melodix/internal/musicapp"
)

const (
	historyCLITitleMaxRunes = 48
	historyCLIURLMaxBytes   = 56
	sectionUnderlineMax     = 80
)

// HistoryCLIRow is one tabular row for CLI history output (tabwriter columns).
type HistoryCLIRow struct {
	ID    uint64
	Title string
	URL   string
	Tail  string // formatted date or play count
}

func historyLinePlain(id uint64, title, url, tail string) string {
	if url != "" {
		return fmt.Sprintf("%d  %s  %s  %s", id, title, url, tail)
	}
	return fmt.Sprintf("%d  %s  %s", id, title, tail)
}

// FormatTimelineLinePlain renders one plain-text line for a timeline row (e.g. tests, debugging).
func FormatTimelineLinePlain(m domain.MusicPlayback) string {
	tail := m.PlayedAt.Format("02 Jan 2006")
	title := history.DisplayTrackTitle(m.Title)
	build := func(tt string) string {
		return historyLinePlain(m.ID, tt, m.URL, tail)
	}
	title = history.FitTitleToLineLimit(title, build)
	return build(title)
}

// FormatCountsLinePlain renders one plain-text line for a counts row.
func FormatCountsLinePlain(r domain.PlaybackCountRow) string {
	tail := fmt.Sprintf("x%d", r.Count)
	title := history.DisplayTrackTitle(r.Title)
	build := func(tt string) string {
		return historyLinePlain(r.RepresentativeID, tt, r.URL, tail)
	}
	title = history.FitTitleToLineLimit(title, build)
	return build(title)
}

func truncateURLMiddle(s string, maxBytes int) string {
	if s == "" {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	if maxBytes <= 3 {
		if maxBytes <= 0 {
			return ""
		}
		return s[:maxBytes]
	}
	inner := maxBytes - 3
	left := inner / 2
	right := inner - left
	return s[:left] + "..." + s[len(s)-right:]
}

func timelineCLIRow(m domain.MusicPlayback) HistoryCLIRow {
	tail := m.PlayedAt.Format("02 Jan 2006")
	title := history.DisplayTrackTitle(m.Title)
	title = history.TruncateTitleMiddle(title, historyCLITitleMaxRunes)
	url := truncateURLMiddle(m.URL, historyCLIURLMaxBytes)
	return HistoryCLIRow{ID: m.ID, Title: title, URL: url, Tail: tail}
}

func countsCLIRow(r domain.PlaybackCountRow) HistoryCLIRow {
	tail := fmt.Sprintf("x%d", r.Count)
	title := history.DisplayTrackTitle(r.Title)
	title = history.TruncateTitleMiddle(title, historyCLITitleMaxRunes)
	url := truncateURLMiddle(r.URL, historyCLIURLMaxBytes)
	return HistoryCLIRow{ID: r.RepresentativeID, Title: title, URL: url, Tail: tail}
}

// PresentHistoryCLI returns title, footer suffix, and tabular rows for terminal output from a history page.
func PresentHistoryCLI(p *musicapp.HistoryPage) (title string, footerExtra string, rows []HistoryCLIRow) {
	if p == nil {
		return "", "", nil
	}
	switch strings.ToLower(strings.TrimSpace(p.View)) {
	case "counts":
		title = TitleHistoryCounts
		footerExtra = FooterHistoryCountsPage()
		for _, r := range p.Counts {
			rows = append(rows, countsCLIRow(r))
		}
	default:
		title = TitleHistoryTimeline
		footerExtra = FooterHistoryTimelinePage()
		for _, m := range p.Rows {
			rows = append(rows, timelineCLIRow(m))
		}
	}
	return title, footerExtra, rows
}

// PrintSectionHeader prints a title and an underline (width capped).
func PrintSectionHeader(w io.Writer, title string) {
	fmt.Fprintln(w, title)
	under := len(title)
	if under > sectionUnderlineMax {
		under = sectionUnderlineMax
	}
	fmt.Fprintln(w, strings.Repeat("-", under))
}

// PrintHistoryPage renders a history page with tabwriter columns and footer line.
func PrintHistoryPage(w io.Writer, page *musicapp.HistoryPage) {
	if page == nil {
		return
	}
	title, footer, rows := PresentHistoryCLI(page)
	PrintSectionHeader(w, title)
	PrintHistoryTable(w, page.View, rows)
	fmt.Fprintf(w, "Page %d/%d (%d rows). %s\n", page.Page, page.TotalPages, page.TotalRows, footer)
}

// PrintHistoryTable writes ID, TITLE, URL, and WHEN/PLAYS columns.
func PrintHistoryTable(w io.Writer, view string, rows []HistoryCLIRow) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	col := "WHEN"
	if view == "counts" {
		col = "PLAYS"
	}
	fmt.Fprintf(tw, "ID\tTITLE\tURL\t%s\n", col)
	for _, r := range rows {
		fmt.Fprintf(tw, "%6d\t%s\t%s\t%s\n", r.ID, r.Title, r.URL, r.Tail)
	}
	_ = tw.Flush()
}
