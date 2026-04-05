package stream

import "errors"

// ErrPlaybackStopped is returned by AudioSink.Stream when the stop channel was closed (user stop / skip),
// as opposed to nil on natural stream end (EOF).
var ErrPlaybackStopped = errors.New("playback stopped")

// ErrVoiceTransport is returned when audio could not be sent to Discord voice (e.g. dead Opus channel).
// It is distinct from ErrPlaybackStopped and from media/source errors handled by RecoveryStream.
var ErrVoiceTransport = errors.New("discord voice transport failed")
