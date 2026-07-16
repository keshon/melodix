package soundcloud

import (
	"errors"

	"github.com/keshon/melodix/pkg/music/soundcloudapi"
)

var ErrNoTrackMatch = errors.New("no track found for the given query")

// Searcher turns a text query into a SoundCloud track URL via api-v2 search.
type Searcher struct {
	api *soundcloudapi.Client
}

func NewSearcher() *Searcher {
	// The shared client keeps one client_id cache with the scnative parser.
	return &Searcher{api: soundcloudapi.Default()}
}

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
