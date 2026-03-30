package history

import (
	"strings"
	"unicode/utf8"
)

// HistoryMaxLineBytes caps rendered line length (embed row); long track titles get middle ellipsis.
const HistoryMaxLineBytes = 120

const historyMinTitleRunes = 8

// DisplayTrackTitle returns a non-empty display title for history rows.
func DisplayTrackTitle(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "(no title)"
	}
	return strings.TrimSpace(raw)
}

// TruncateTitleMiddle shortens s to at most maxRunes runes, inserting "..." in the middle when needed.
func TruncateTitleMiddle(s string, maxRunes int) string {
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

// FitTitleToLineLimit shortens title until build(title) fits within HistoryMaxLineBytes.
func FitTitleToLineLimit(title string, build func(string) string) string {
	if len(build(title)) <= HistoryMaxLineBytes {
		return title
	}
	n := utf8.RuneCountInString(title)
	for max := n; max >= historyMinTitleRunes; max-- {
		short := TruncateTitleMiddle(title, max)
		if len(build(short)) <= HistoryMaxLineBytes {
			return short
		}
	}
	return TruncateTitleMiddle(title, historyMinTitleRunes)
}
