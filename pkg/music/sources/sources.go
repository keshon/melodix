// Package sources defines the Source interface and track types used by the resolver.
package sources

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
