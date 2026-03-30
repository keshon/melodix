package domain

import "errors"

// ErrMusicPlaybackNotFound is returned when no history row matches the id (unknown, trimmed, or typo).
var ErrMusicPlaybackNotFound = errors.New("music playback not found")
