// Package tracklist renders tracks as plain terminal lines -- the queue,
// search results, history -- the way the bot's tracklist renders them as
// Discord markdown.
package tracklist

import (
	"fmt"
	"strings"
	"time"

	"github.com/keshon/melodix/pkg/music/parsers"
)

// queueLinesShown caps how many upcoming tracks the queue lists, as the bot's
// queue view does.
const queueLinesShown = 15

// Queue renders the now-playing line, the first queueLinesShown upcoming
// tracks and a remainder count. current may be nil.
func Queue(current *parsers.Track, upcoming []parsers.Track) string {
	var b strings.Builder
	if current != nil {
		b.WriteString("▶ " + label(current.Title, current.URL, current.Duration) + "\n")
	}
	if len(upcoming) == 0 {
		if current == nil {
			return "The queue is empty."
		}
		return strings.TrimRight(b.String(), "\n")
	}
	shown := upcoming
	if len(shown) > queueLinesShown {
		shown = shown[:queueLinesShown]
	}
	for i, t := range shown {
		b.WriteString(Line(i+1, t.Title, t.URL, t.Duration) + "\n")
	}
	if rest := len(upcoming) - len(shown); rest > 0 {
		fmt.Fprintf(&b, "…and %d more\n", rest)
	}
	return strings.TrimRight(b.String(), "\n")
}

// Line renders one numbered track.
func Line(pos int, title, url string, d time.Duration) string {
	return fmt.Sprintf("%3d. %s", pos, label(title, url, d))
}

// SearchLine renders one numbered search hit, tailed with the uploader.
func SearchLine(pos int, title, url, author string, d time.Duration) string {
	line := Line(pos, title, url, d)
	if a := strings.TrimSpace(author); a != "" {
		line += " — " + a
	}
	return line
}

// TimelineLine renders one history row: its id, the track and when it played.
func TimelineLine(id uint64, title, url string, playedAt time.Time) string {
	return fmt.Sprintf("#%-5d %s  %s", id, label(title, url, 0), playedAt.Local().Format("2006-01-02 15:04"))
}

// CountsLine renders one row of the by-URL view.
func CountsLine(id uint64, title, url string, count int) string {
	return fmt.Sprintf("#%-5d %s  ×%d", id, label(title, url, 0), count)
}

// label is a track's title, or its URL when the title is not known yet, with
// its duration when that is.
func label(title, url string, d time.Duration) string {
	name := strings.TrimSpace(title)
	if name == "" {
		name = strings.TrimSpace(url)
	}
	if name == "" {
		name = "(no title)"
	}
	if d > 0 {
		name += " (" + duration(d) + ")"
	}
	return name
}

func duration(d time.Duration) string {
	total := int(d.Round(time.Second).Seconds())
	h, m, s := total/3600, total/60%60, total%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}
