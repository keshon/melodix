// Package stream opens tracks as Opus packet streams and adds recovery.
package stream

import (
	"fmt"
	"sync/atomic"

	"github.com/keshon/melodix/pkg/music/cache"
	"github.com/keshon/melodix/pkg/music/opus"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/parsers/ffmpeg"
	"github.com/keshon/melodix/pkg/music/parsers/kkdai"
	"github.com/keshon/melodix/pkg/music/parsers/scnative"
	"github.com/keshon/melodix/pkg/music/parsers/ytdlp"
	"github.com/keshon/melodix/pkg/music/parsers/ytnative"
	"github.com/keshon/melodix/pkg/music/sources"
)

// registryEntries maps parser keys (the sources.Parser* constants; the strings
// are persisted in playback history, so keys are frozen identifiers) to
// streamers. The kkdai/ytdlp link-vs-pipe modes are carried on the streamer
// instance. To add a parser, add one entry here.
var registryEntries = map[string]parsers.Streamer{
	sources.ParserYtnativeLink: &ytnative.Streamer{},
	sources.ParserScnativeLink: &scnative.Streamer{},
	sources.ParserKkdaiLink:    &kkdai.Streamer{Mode: kkdai.ModeLink},
	sources.ParserKkdaiPipe:    &kkdai.Streamer{Mode: kkdai.ModePipe},
	sources.ParserYtdlpLink:    &ytdlp.Streamer{Mode: ytdlp.ModeLink},
	sources.ParserYtdlpPipe:    &ytdlp.Streamer{Mode: ytdlp.ModePipe},
	sources.ParserFFmpegLink:   &ffmpeg.Streamer{},
}

// registry holds the active parser registry behind an atomic pointer.
// Production stores registryEntries once at init and never swaps; tests swap in
// fakes via SetRegistry. Atomic access keeps that swap race-free against player
// goroutines that read the registry while opening a stream.
var registry atomic.Pointer[map[string]parsers.Streamer]

func init() { registry.Store(&registryEntries) }

// Registry returns the active parser registry. Treat the result as read-only.
func Registry() map[string]parsers.Streamer { return *registry.Load() }

// SetRegistry swaps the active registry and returns the previous one. Intended
// for tests, which restore the original in a cleanup/defer.
func SetRegistry(r map[string]parsers.Streamer) map[string]parsers.Streamer {
	return *registry.Swap(&r)
}

// activeCache and bufferAheadPackets are optional playback layers, set once at
// boot (nil / 0 = disabled). They are never swapped after startup, so plain
// package vars are safe — unlike the test-swapped Registry, which is atomic.
var (
	activeCache        *cache.Store
	bufferAheadPackets int
)

// SetCache installs the global track cache used by RecoveryStream (nil disables
// caching). Call once at startup, before playback.
func SetCache(c *cache.Store) { activeCache = c }

// SetBufferAhead sets the anti-skip read-ahead depth in milliseconds (<=0
// disables the buffer). Call once at startup.
func SetBufferAhead(ms int) {
	if ms <= 0 {
		bufferAheadPackets = 0
		return
	}
	bufferAheadPackets = ms / opus.FrameMs
}

// bufferWrap adds the anti-skip read-ahead buffer around a reader, composing the
// producer stop into the returned cleanup.
func bufferWrap(reader opus.Reader, cleanup func()) (opus.Reader, func()) {
	if bufferAheadPackets <= 0 {
		return reader, cleanup
	}
	if buf, ok := opus.NewBufferedReader(reader, bufferAheadPackets).(*opus.BufferedReader); ok {
		return buf, func() { buf.Stop(); cleanup() }
	}
	return reader, cleanup
}

// openWithParser opens the Opus packet stream for the given parser key.
func openWithParser(track *parsers.Track, parser string, seekSec float64) (opus.Reader, func(), error) {
	streamer, ok := Registry()[parser]
	if !ok {
		return nil, nil, fmt.Errorf("stream: no streamer for parser %q", parser)
	}
	return streamer.Open(track, seekSec)
}
