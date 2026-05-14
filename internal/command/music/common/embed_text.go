package common

import (
	"github.com/keshon/melodix/internal/playbackerr"
)

// PlaybackErrorString applies the same length limits as PlaybackErrorDescription for a raw message.
func PlaybackErrorString(s string) string {
	return playbackerr.String(s)
}

// PlaybackErrorDescription formats an error for a Discord embed description (length-capped).
func PlaybackErrorDescription(err error) string {
	if err == nil {
		return "Unknown error."
	}
	return playbackerr.String(err.Error())
}
