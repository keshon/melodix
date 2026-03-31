// Package sources defines the Source interface and track types used by the resolver.
package sources

const (
	Auto       = "auto"
	YouTube    = "youtube"
	Radio      = "radio"
	SoundCloud = "soundcloud"
)

type TrackInfo struct {
	URL              string
	Title            string
	SourceName       string
	AvailableParsers []string
}
