// Package sink defines interfaces and implementations for consuming a track's
// Opus packet stream (e.g. forward to Discord voice, or decode to a speaker).
package sink

import "github.com/keshon/melodix/pkg/music/opus"

// AudioSink consumes a stream of 20ms Opus packets. The sink owns the read loop;
// Stream returns when the stream ends (io.EOF) or stop is closed.
type AudioSink interface {
	Stream(r opus.Reader, stop <-chan struct{}) error
}

// Provider returns an AudioSink for a given target.
// For Discord, target is the voice channel ID; for CLI, target is typically "".
type Provider interface {
	Sink(target string) (AudioSink, error)
	// ReleaseSink is called when the player disconnects (e.g. Stop(true)).
	// Discord uses it to leave the voice channel; CLI can no-op.
	ReleaseSink(target string)
	// InvalidateSink drops any cached voice/transport state so the next Sink re-acquires it
	// (e.g. after gateway reconnect or Opus send failure).
	InvalidateSink()
}
