// Package playbackerr holds shared formatting for user-visible playback errors (Discord embeds, etc.).
// Kept separate from internal/command/music/common to avoid import cycles with internal/discord/voice.
package playbackerr

// String applies a rune-length cap suitable for Discord embed descriptions.
func String(s string) string {
	if s == "" {
		return ""
	}
	const maxRunes = 3500
	r := []rune(s)
	if len(r) <= maxRunes {
		return string(r)
	}
	return string(r[:maxRunes]) + "…"
}
