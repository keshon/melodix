package common

import (
	"fmt"
	"strings"
	"time"

	"github.com/keshon/melodix/pkg/music/parsers"
)

// queueLinesShown caps how many upcoming tracks a queue view lists. The queue is
// transient — it drains as tracks play — so the overflow is a count rather than
// pagination: there is no stable page to come back to.
const queueLinesShown = 15

// FormatQueueBody renders the /queue embed body: the now-playing line, the first
// queueLinesShown upcoming rows, and a remainder count. current may be nil.
//
// Queued tracks have not been opened yet, so Title and Duration are whatever the
// resolver supplied — often an empty title and a zero duration, since parsers
// fill both at open time. Every field is therefore treated as optional.
func FormatQueueBody(current *parsers.Track, upcoming []parsers.Track) string {
	var b strings.Builder

	if current != nil {
		b.WriteString("▶️ " + trackLabel(current.Title, current.URL, current.Duration))
	}

	if len(upcoming) == 0 {
		if current == nil {
			return "The queue is empty. Add something with `/play`."
		}
		b.WriteString("\n\nNothing queued after this.")
		return b.String()
	}

	if current != nil {
		// Blank line: the only vertical spacing embed markdown offers.
		b.WriteString("\n\n")
	}

	shown := upcoming
	if len(shown) > queueLinesShown {
		shown = shown[:queueLinesShown]
	}
	lines := make([]string, 0, len(shown))
	for i, t := range shown {
		lines = append(lines, FormatQueueLine(i+1, t.Title, t.URL, t.Duration))
	}
	b.WriteString(strings.Join(lines, "\n"))

	if rest := len(upcoming) - len(shown); rest > 0 {
		fmt.Fprintf(&b, "\n\n…and %d more", rest)
	}
	return b.String()
}

// FormatQueueLine renders one upcoming row: `pos` [title](url) `duration`.
func FormatQueueLine(pos int, title, url string, d time.Duration) string {
	return fmt.Sprintf("`%d` %s", pos, trackLabel(title, url, d))
}

// trackLabel renders a track as a link with an optional duration chip, falling
// back to the URL when there is no title and to a placeholder when there is
// neither. Long titles get the same middle ellipsis as history rows.
func trackLabel(title, url string, d time.Duration) string {
	name := strings.TrimSpace(title)
	if name == "" {
		name = strings.TrimSpace(url)
	}
	if name == "" {
		return "(no title)"
	}

	var tail string
	if d > 0 {
		tail = " `" + formatQueueDuration(d) + "`"
	}
	build := func(tt string) string {
		if url == "" {
			return tt + tail
		}
		return "[" + tt + "](" + url + ")" + tail
	}
	return build(fitTitleToLineLimit(name, build))
}

// formatQueueDuration mirrors the duration chip in reply.NowPlayingEmbed. It is
// duplicated rather than shared because this package is deliberately free of
// discordgo, and reply's copy is not reachable without pulling it in.
func formatQueueDuration(d time.Duration) string {
	total := int(d.Round(time.Second).Seconds())
	h, m, s := total/3600, total/60%60, total%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}
