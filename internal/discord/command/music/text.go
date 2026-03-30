package music

import "fmt"

// Discord embed copy: titles, footers, and status strings. Emoji use Unicode escapes for stable source encoding.
const (
	TitleError             = "\U0001f3b5 Error"               // 🎵 Error
	TitleVoiceError        = "\U0001f3b5 Voice Error"        // 🎵 Voice Error
	TitleHistoryShort      = "\U0001f3b5 History"            // 🎵 History — ephemeral errors / empty state
	TitleQueueError        = "\U0001f3b5 Queue Error"       // 🎵 Queue Error
	TitleVoiceChannelError = "\U0001f3b5 Voice Channel Error" // 🎵 Voice Channel Error
	TitleQueueEmpty        = "\U0001f3b5 Queue Empty"        // 🎵 Queue Empty
	TitlePlaybackError     = "\U0001f3b5 Playback Error"     // 🎵 Playback Error
	TitleWarnError         = "\u26a0\ufe0f Error"           // ⚠️ Error

	// TitleHistoryTimeline / TitleHistoryCounts are full embed titles for the history list.
	TitleHistoryTimeline = "\U0001f3b5 Playback history (timeline)"
	TitleHistoryCounts   = "\U0001f3b5 Playback history (by URL)"

	// FooterReplayHint is the replay line in embed footers and the counts-only footer suffix.
	FooterReplayHint = "replay with `/music play <id>`."

	// PrefixStop is ⏹️ plus space for stop confirmations.
	PrefixStop = "\u23f9\ufe0f "

	// MsgHistoryEmpty is the body when there are no rows yet.
	MsgHistoryEmpty = "No playback history yet. Use `/music play` first. History is stored per server; very old entries may be removed when the list is trimmed."
)

// FooterHistoryTimelinePage is the footer suffix for timeline view (chronological + replay hint).
func FooterHistoryTimelinePage() string {
	return "Chronological; " + FooterReplayHint
}

// FooterHistoryCountsPage is the footer suffix for counts view (replay hint only).
func FooterHistoryCountsPage() string {
	return FooterReplayHint
}

// NowPlayingMarkdown formats the status line with 🎶 for Discord markdown.
func NowPlayingMarkdown(label, url string) string {
	const notes = "\U0001f3b6 " // 🎶
	if url != "" {
		return fmt.Sprintf("%s[%s](%s)", notes, label, url)
	}
	return notes + label
}
