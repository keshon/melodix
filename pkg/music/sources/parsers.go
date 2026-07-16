package sources

// Registry keys for the built-in parsers (this package is the import base for
// both sources and parsers, so the names live here). These strings are persisted
// in guild playback history and shown as slash-command choices — treat them as
// frozen identifiers: never rename an existing key, only add new ones.
const (
	ParserYtnativeLink = "ytnative-link"
	ParserScnativeLink = "scnative-link"
	ParserKkdaiLink    = "kkdai-link"
	ParserKkdaiPipe    = "kkdai-pipe"
	ParserYtdlpLink    = "ytdlp-link"
	ParserYtdlpPipe    = "ytdlp-pipe"
	ParserFFmpegLink   = "ffmpeg-link"
)

// PreferParser returns a new slice where selected is first (if present).
// If selected is empty or not in available, it returns available as-is.
func PreferParser(available []string, selected string) []string {
	if len(available) == 0 || selected == "" {
		return available
	}

	pos := -1
	for i, v := range available {
		if v == selected {
			pos = i
			break
		}
	}
	if pos <= 0 {
		return available
	}

	ordered := make([]string, 0, len(available))
	ordered = append(ordered, selected)
	ordered = append(ordered, available[:pos]...)
	ordered = append(ordered, available[pos+1:]...)
	return ordered
}
