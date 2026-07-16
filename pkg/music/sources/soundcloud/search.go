package soundcloud

import (
	"errors"

	"github.com/keshon/melodix/pkg/music/soundcloudapi"
)

// ErrNoTrackMatch means the search returned no usable track.
var ErrNoTrackMatch = errors.New("no track found for the given query")

// Searcher turns a text query into a SoundCloud track URL via api-v2 search.
type Searcher struct {
	api *soundcloudapi.Client
}

// NewSearcher creates a Searcher backed by the shared api-v2 client.
func NewSearcher() *Searcher {
	// The shared client keeps one client_id cache with the scnative parser.
	return &Searcher{api: soundcloudapi.Default()}
}

// SearchFirstTrackURL returns the permalink URL of the top search result.
func (r *Searcher) SearchFirstTrackURL(query string) (string, error) {
	track, err := r.api.SearchFirstTrack(query)
	if err != nil {
		if errors.Is(err, soundcloudapi.ErrNoResults) {
			return "", ErrNoTrackMatch
		}
		return "", err
	}
	if track.PermalinkURL == "" {
		return "", ErrNoTrackMatch
	}
	return track.PermalinkURL, nil
}
