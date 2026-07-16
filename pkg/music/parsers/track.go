// Package parsers defines the Streamer interface and the playback Track type
// for opening PCM streams from URLs.
package parsers

import (
	"time"

	"github.com/keshon/melodix/pkg/music/sources"
)

// Track is the playback entity that flows through the queue, parsers, and sinks.
// It starts as a thin copy of the resolver's TrackInfo; parsers fill in Title,
// Artist and Duration at open time, and recovery updates CurrentParser as
// fallbacks engage.
type Track struct {
	URL      string
	Title    string
	Artist   string
	Duration time.Duration
	// CurrentParser is the registry key of the parser currently playing this
	// track (starts as the first preference, updated by recovery fallback).
	CurrentParser string
	// Passthrough is true when the active stream forwards native Opus packets
	// with no ffmpeg/transcode (set by the parser at open time; reset per attempt).
	Passthrough bool
	// SourceInfo is the resolver's original metadata, including the ordered
	// parser preference list recovery iterates over.
	SourceInfo sources.TrackInfo
}
