// Package parsers defines the Streamer interface and the playback Track type.
// A Streamer opens a track as 20ms Opus packets (opus.Reader); PCM appears only
// inside the ffmpeg-backed parsers, as an intermediate on the way to Opus.
package parsers

import (
	"slices"
	"time"

	"github.com/keshon/melodix/pkg/music/sources"
)

// Track is the playback entity that flows through the queue, parsers, and
// sinks. It starts as a thin copy of the resolver's TrackInfo; parsers fill in
// Title, Artist and Duration at open time, and recovery updates CurrentParser
// as fallbacks engage.
//
// A Track has exactly one writer at a time, and it is whoever is driving the
// stream it describes. Anything that merely reads one -- a queue listing, a
// status embed, a history row -- takes a Clone, because the fields a parser
// fills in are written while that reader may be rendering. Handing out the
// same Track to a reader and to an opener is the one way to misuse this type.
type Track struct {
	URL      string
	Title    string
	Artist   string
	Duration time.Duration
	// CurrentParser is the registry key of the parser currently playing this
	// track (starts as the first preference, updated by recovery fallback).
	CurrentParser string
	// Passthrough is true when the active stream forwards native Opus packets with
	// no ffmpeg/transcode (set by the parser at open time; reset per attempt).
	Passthrough bool
	// Cached is true when the active stream is served from the local track cache
	// (set by RecoveryStream at open; not persisted).
	Cached bool
	// SourceInfo is the resolver's original metadata, including the ordered
	// parser preference list recovery iterates over.
	SourceInfo sources.TrackInfo
}

// Clone returns a copy safe to hand to a reader: the slice the queue and
// recovery both iterate is copied too, so the caller cannot write through it.
func (t Track) Clone() Track {
	out := t
	if len(t.SourceInfo.AvailableParsers) > 0 {
		out.SourceInfo.AvailableParsers = slices.Clone(t.SourceInfo.AvailableParsers)
	}
	return out
}
