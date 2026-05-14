package common

// PlaybackErrorDescription formats an error for a Discord embed description (length-capped).
func PlaybackErrorDescription(err error) string {
	if err == nil {
		return "Unknown error."
	}
	const maxRunes = 3500
	r := []rune(err.Error())
	if len(r) <= maxRunes {
		return string(r)
	}
	return string(r[:maxRunes]) + "…"
}
