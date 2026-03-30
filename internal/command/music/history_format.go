package music

import (
	"fmt"
	"strings"
	"unicode/utf8"

	appmusic "github.com/keshon/melodix/internal/app/music"
	"github.com/keshon/melodix/internal/domain"
)

const (
	historyMaxLineBytes = 120
	historyMinTitleRunes = 8

	// HistoryFooterReplay is shown in /music history footer for replay hint (counts view uses the same sentence as timeline).
	HistoryFooterReplay = "replay with `/music play <id>`."

	historyEmbedTitleTimeline = "🎵 Playback history (timeline)"
	historyEmbedTitleCounts   = "🎵 Playback history (by URL)"
)

func historyEmbedTitle(view string) string {
	if strings.EqualFold(strings.TrimSpace(view), "counts") {
		return historyEmbedTitleCounts
	}
	return historyEmbedTitleTimeline
}

func historyFooterExtra(view string) string {
	if strings.EqualFold(strings.TrimSpace(view), "counts") {
		return HistoryFooterReplay
	}
	return "Chronological; " + HistoryFooterReplay
}

// FormatHistoryLines turns a history page into Discord markdown lines (embed description).
func FormatHistoryLines(data appmusic.HistoryPageData) []string {
	switch data.View {
	case "counts":
		lines := make([]string, 0, len(data.CountsPage))
		for _, r := range data.CountsPage {
			lines = append(lines, formatCountsLine(r))
		}
		return lines
	default:
		lines := make([]string, 0, len(data.TimelinePage))
		for _, m := range data.TimelinePage {
			lines = append(lines, formatTimelineLine(m))
		}
		return lines
	}
}

func displayTrackTitle(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "(no title)"
	}
	return strings.TrimSpace(raw)
}

func truncateTitleMiddle(s string, maxRunes int) string {
	if maxRunes < 1 {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	if maxRunes <= 3 {
		return string(r[:maxRunes])
	}
	inner := maxRunes - 3
	left := inner / 2
	right := inner - left
	return string(r[:left]) + "..." + string(r[len(r)-right:])
}

func fitTitleToLineLimit(title string, build func(string) string) string {
	if len(build(title)) <= historyMaxLineBytes {
		return title
	}
	n := utf8.RuneCountInString(title)
	for max := n; max >= historyMinTitleRunes; max-- {
		short := truncateTitleMiddle(title, max)
		if len(build(short)) <= historyMaxLineBytes {
			return short
		}
	}
	return truncateTitleMiddle(title, historyMinTitleRunes)
}

func historyLine(id uint64, title, url, tail string) string {
	if url != "" {
		return fmt.Sprintf("`%d` [%s](%s) `%s`", id, title, url, tail)
	}
	return fmt.Sprintf("`%d` %s `%s`", id, title, tail)
}

func formatTimelineLine(m domain.MusicPlayback) string {
	tail := m.PlayedAt.Format("02 Jan 2006")
	title := displayTrackTitle(m.Title)
	build := func(tt string) string {
		return historyLine(m.ID, tt, m.URL, tail)
	}
	title = fitTitleToLineLimit(title, build)
	return build(title)
}

func formatCountsLine(r domain.PlaybackCountRow) string {
	tail := fmt.Sprintf("×%d", r.Count)
	title := displayTrackTitle(r.Title)
	build := func(tt string) string {
		return historyLine(r.RepresentativeID, tt, r.URL, tail)
	}
	title = fitTitleToLineLimit(title, build)
	return build(title)
}
