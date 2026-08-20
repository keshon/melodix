// Package sources defines the Source interface and track types used by the resolver.
package sources

import "time"

// Source name identifiers, used for source selection and persisted in playback history.
const (
	Auto       = "auto"
	YouTube    = "youtube"
	Radio      = "radio"
	SoundCloud = "soundcloud"
)

// TrackInfo is a resolver's product: page-level track metadata plus an ordered
// parser preference list. It deliberately carries no stream URLs — those expire,
// so parsers resolve them lazily at open time.
type TrackInfo struct {
	URL              string
	Title            string
	SourceName       string
	AvailableParsers []string
}

// SearchResult is one hit from a source's ranked search, shaped for a chooser
// UI rather than for playback.
type SearchResult struct {
	// ID is the source's own compact identifier for the track — a YouTube video
	// id, a SoundCloud track id. It exists separately from URL because it has to
	// survive a round trip through a Discord component id, where a full URL does
	// not reliably fit.
	ID string
	// URL is the canonical page for the track, used for display links.
	URL string

	Title  string
	Author string
	// Duration is zero when the source reports none, which is what a live
	// stream looks like.
	Duration time.Duration
}

// Searcher is implemented by sources that offer a ranked search worth choosing
// from. It is deliberately not part of Source: radio has nothing to rank, and
// resolving a query to one track is a different job from listing candidates.
type Searcher interface {
	// Search returns at most limit hits in the source's own ranking.
	Search(query string, limit int) ([]SearchResult, error)
}
