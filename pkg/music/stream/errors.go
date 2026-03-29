package stream

import "errors"

// ErrPlaybackStopped is returned by AudioSink.Stream when the stop channel was closed (user stop / skip),
// as opposed to nil on natural stream end (EOF).
var ErrPlaybackStopped = errors.New("playback stopped")
