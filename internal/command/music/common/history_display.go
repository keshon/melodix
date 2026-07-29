package common

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// historyMaxLineBytes caps rendered line length (embed row); long track titles get middle ellipsis.
const historyMaxLineBytes = 120

const historyMinTitleRunes = 8

func displayTrackTitle(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "(no title)"
	}
	return strings.TrimSpace(raw)
}

// truncateTitleMiddle shortens s to at most maxRunes runes, inserting "..." in the middle when needed.
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

// historyLine: `id` [title](url) `tail` (spaces only; tail is backtick-wrapped date or ×N play count).
func historyLine(id uint64, title, url, tail string) string {
	if url != "" {
		return fmt.Sprintf("`%d` [%s](%s) `%s`", id, title, url, tail)
	}
	return fmt.Sprintf("`%d` %s `%s`", id, title, tail)
}

// FormatTimelineLine renders one chronological history row. It takes plain
// values rather than a persisted struct so this presentation package stays
// independent of the storage schema.
func FormatTimelineLine(id uint64, title, url string, playedAt time.Time) string {
	tail := playedAt.Format("02 Jan 2006")
	name := displayTrackTitle(title)
	build := func(tt string) string {
		return historyLine(id, tt, url, tail)
	}
	name = fitTitleToLineLimit(name, build)
	return build(name)
}

// FormatCountsLine renders one row of the "by URL" view, tailed with the play count.
func FormatCountsLine(id uint64, title, url string, count int) string {
	tail := fmt.Sprintf("×%d", count)
	name := displayTrackTitle(title)
	build := func(tt string) string {
		return historyLine(id, tt, url, tail)
	}
	name = fitTitleToLineLimit(name, build)
	return build(name)
}
