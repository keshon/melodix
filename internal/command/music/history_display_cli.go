package music

import (
	"fmt"

	"github.com/keshon/melodix/internal/domain"
)

const (
	historyCLITitleMaxRunes = 48
	historyCLIURLMaxBytes   = 56
)

func historyLinePlain(id uint64, title, url, tail string) string {
	if url != "" {
		return fmt.Sprintf("%d  %s  %s  %s", id, title, url, tail)
	}
	return fmt.Sprintf("%d  %s  %s", id, title, tail)
}

func formatTimelineLinePlain(m domain.MusicPlayback) string {
	tail := m.PlayedAt.Format("02 Jan 2006")
	title := displayTrackTitle(m.Title)
	build := func(tt string) string {
		return historyLinePlain(m.ID, tt, m.URL, tail)
	}
	title = fitTitleToLineLimit(title, build)
	return build(title)
}

func formatCountsLinePlain(r domain.PlaybackCountRow) string {
	tail := fmt.Sprintf("x%d", r.Count)
	title := displayTrackTitle(r.Title)
	build := func(tt string) string {
		return historyLinePlain(r.RepresentativeID, tt, r.URL, tail)
	}
	title = fitTitleToLineLimit(title, build)
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
	title := displayTrackTitle(m.Title)
	title = truncateTitleMiddle(title, historyCLITitleMaxRunes)
	url := truncateURLMiddle(m.URL, historyCLIURLMaxBytes)
	return HistoryCLIRow{ID: m.ID, Title: title, URL: url, Tail: tail}
}

func countsCLIRow(r domain.PlaybackCountRow) HistoryCLIRow {
	tail := fmt.Sprintf("x%d", r.Count)
	title := displayTrackTitle(r.Title)
	title = truncateTitleMiddle(title, historyCLITitleMaxRunes)
	url := truncateURLMiddle(r.URL, historyCLIURLMaxBytes)
	return HistoryCLIRow{ID: r.RepresentativeID, Title: title, URL: url, Tail: tail}
}
