package stream

import (
	"time"

	"github.com/keshon/melodix/pkg/music/parsers"
)

// OpenInfo is what a stream learned about a track by opening it: which parser
// is actually carrying the audio, how, and whatever metadata that parser
// filled in on the way.
//
// It exists so those facts can travel back to the player as a value. They used
// to be written straight into the caller's Track, from whichever goroutine
// happened to be driving the reads -- which is the same Track the status embed
// renders, so a mid-track parser switch raced the UI. A value has one writer by
// construction.
type OpenInfo struct {
	// Parser is the registry key of the parser now carrying the track, or ""
	// when the audio comes from the cache rather than from a parser.
	Parser string
	// Passthrough reports that native Opus packets are being forwarded with no
	// transcode.
	Passthrough bool
	// Cached reports that the audio is being served from the local track cache.
	Cached bool
	// Title, Artist and Duration are the parser's view of the track. They are
	// often better than the resolver's -- a search result carries what the
	// search page said, the parser carries what the media itself says -- and
	// are empty when the parser had nothing to add.
	Title    string
	Artist   string
	Duration time.Duration
}

// Apply writes this stream's findings onto a track, leaving fields the parser
// had nothing to say about alone. The caller decides when that is safe, which
// is the point: the player does it under its own lock.
func (i OpenInfo) Apply(track *parsers.Track) {
	if track == nil {
		return
	}
	track.CurrentParser = i.Parser
	track.Passthrough = i.Passthrough
	track.Cached = i.Cached
	if i.Title != "" {
		track.Title = i.Title
	}
	if i.Artist != "" {
		track.Artist = i.Artist
	}
	if i.Duration > 0 {
		track.Duration = i.Duration
	}
}

// openInfo snapshots the stream's own track. Called only from the goroutine
// that owns it: on the Open path from whoever called Open, and on the confirm
// path from the reader.
func (rs *RecoveryStream) openInfo() OpenInfo {
	return OpenInfo{
		Parser:      rs.track.CurrentParser,
		Passthrough: rs.track.Passthrough,
		Cached:      rs.track.Cached,
		Title:       rs.track.Title,
		Artist:      rs.track.Artist,
		Duration:    rs.track.Duration,
	}
}
