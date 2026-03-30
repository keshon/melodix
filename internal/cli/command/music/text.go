package music

// CLI terminal copy — parallel names to [internal/discord/command/music] (plain text, no emoji in titles).
const (
	TitleHistoryTimeline = "Playback history (timeline)"
	TitleHistoryCounts   = "Playback history (by URL)"

	// FooterReplayHint is the replay line in the history footer.
	FooterReplayHint = "replay with: play <id>."

	// MsgHistoryEmpty is printed when there are no rows yet.
	MsgHistoryEmpty = "No playback history yet. Use play first. Old entries may be removed when the list is trimmed."
)

// FooterHistoryTimelinePage is the footer line suffix for timeline view.
func FooterHistoryTimelinePage() string {
	return "Chronological; " + FooterReplayHint
}

// FooterHistoryCountsPage is the footer line suffix for counts view.
func FooterHistoryCountsPage() string {
	return FooterReplayHint
}
