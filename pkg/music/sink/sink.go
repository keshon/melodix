package sink

import "io"

// AudioSink consumes a PCM stream (e.g. encode-and-send to Discord VC, or play to speaker).
// The sink owns the read loop; Stream returns when the stream ends or stop is closed.
type AudioSink interface {
	Stream(stream io.ReadCloser, stop <-chan struct{}) error
}

// SinkProvider returns an AudioSink for a given target.
// For Discord, target is the voice channel ID; for CLI, target is typically "".
type SinkProvider interface {
	GetSink(target string) (AudioSink, error)
	// ReleaseSink is called when the player disconnects (e.g. Stop(true)).
	// Discord uses it to leave the voice channel; CLI can no-op.
	ReleaseSink(target string)
}
