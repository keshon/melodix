// Package parsers defines the Streamer interface and track type for opening PCM streams from URLs.
package parsers

import (
	"strings"
	"time"

	"github.com/keshon/melodix/pkg/music/sources"
)

type TrackParse struct {
	URL                 string
	Title               string
	Artist              string
	Duration            time.Duration
	CurrentPlayDuration time.Duration
	CurrentParser       string
	SourceInfo          sources.TrackInfo
}

// DisplayLabel returns a short label for UI. Title is often empty until the stream opens
// (metadata is filled by the parser during Open); this falls back to SourceInfo.Title, then URL.
func (t TrackParse) DisplayLabel() string {
	if s := strings.TrimSpace(t.Title); s != "" {
		return s
	}
	if s := strings.TrimSpace(t.SourceInfo.Title); s != "" {
		return s
	}
	if s := strings.TrimSpace(t.URL); s != "" {
		return s
	}
	return "(no title)"
}
