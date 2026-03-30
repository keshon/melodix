package music

import (
	"fmt"

	"github.com/keshon/melodix/internal/domain"
	"github.com/keshon/melodix/internal/history"
)

// historyLine: `id` [title](url) `tail` (spaces only; tail is backtick-wrapped date or ×N play count).
func historyLine(id uint64, title, url, tail string) string {
	if url != "" {
		return fmt.Sprintf("`%d` [%s](%s) `%s`", id, title, url, tail)
	}
	return fmt.Sprintf("`%d` %s `%s`", id, title, tail)
}

// FormatTimelineLine renders one Discord markdown line for a timeline playback row.
func FormatTimelineLine(m domain.MusicPlayback) string {
	tail := m.PlayedAt.Format("02 Jan 2006")
	title := history.DisplayTrackTitle(m.Title)
	build := func(tt string) string {
		return historyLine(m.ID, tt, m.URL, tail)
	}
	title = history.FitTitleToLineLimit(title, build)
	return build(title)
}

// FormatCountsLine renders one Discord markdown line for an aggregated URL/count row.
func FormatCountsLine(r domain.PlaybackCountRow) string {
	tail := fmt.Sprintf("×%d", r.Count)
	title := history.DisplayTrackTitle(r.Title)
	build := func(tt string) string {
		return historyLine(r.RepresentativeID, tt, r.URL, tail)
	}
	title = history.FitTitleToLineLimit(title, build)
	return build(title)
}
