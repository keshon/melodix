// Package sink defines interfaces and implementations for consuming a track's
// Opus packet stream (e.g. forward to Discord voice, or decode to a speaker).
package sink

import "github.com/keshon/melodix/pkg/music/opus"

// AudioSink consumes a stream of 20ms Opus packets. The sink owns the read
// loop; Stream returns when the stream ends (io.EOF) or stop is closed.
//
// Two obligations beyond the happy path, because both were properties of an
// implementation rather than of this contract and a backend swap silently
// removed them:
//
//   - A sink MUST NOT emit a frame its transport cannot protect. Where the
//     transport offers end-to-end encryption, a frame sent while the key
//     exchange has no live epoch is dropped by every receiver expecting
//     encryption, and reaches the server with no end-to-end layer at all.
//     Hold such frames; do not send them and do not discard them.
//   - A sink MUST report the death of its transport as
//     stream.ErrVoiceTransport, and must detect that itself rather than wait
//     for the transport to volunteer it. Stream blocking forever is not a
//     permitted outcome: the player cannot advance a queue, end a track or
//     recover a connection it is never told about.
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
	// InvalidateSink drops any cached voice/transport state so the next Sink
	// re-acquires it (e.g. after gateway reconnect or Opus send failure).
	InvalidateSink()
}
