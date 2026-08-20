package soundcloud

import (
	"errors"
	"strconv"
	"time"

	"github.com/keshon/melodix/pkg/music/soundcloudapi"
	source "github.com/keshon/melodix/pkg/music/sources"
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

// Search returns up to limit tracks for a query, in SoundCloud's own ranking.
//
// ID carries the numeric track id rather than the permalink: permalinks run
// past 130 characters, which does not survive a Discord component id.
func (r *Searcher) Search(query string, limit int) ([]source.SearchResult, error) {
	tracks, err := r.api.SearchTracks(query, limit)
	if err != nil {
		if errors.Is(err, soundcloudapi.ErrNoResults) {
			return nil, ErrNoTrackMatch
		}
		return nil, err
	}

	out := make([]source.SearchResult, 0, len(tracks))
	for _, t := range tracks {
		if t.ID == 0 || t.PermalinkURL == "" {
			continue
		}
		out = append(out, source.SearchResult{
			ID:       strconv.FormatInt(t.ID, 10),
			URL:      t.PermalinkURL,
			Title:    t.Title,
			Author:   t.User.Username,
			Duration: time.Duration(t.DurationMS) * time.Millisecond,
		})
	}
	if len(out) == 0 {
		return nil, ErrNoTrackMatch
	}
	return out, nil
}

// PermalinkByID turns a track id from a search result back into the page URL
// the parsers resolve against.
func (r *Searcher) PermalinkByID(id string) (string, error) {
	track, err := r.api.TrackByID(id)
	if err != nil {
		if errors.Is(err, soundcloudapi.ErrNoResults) {
			return "", ErrNoTrackMatch
		}
		return "", err
	}
	return track.PermalinkURL, nil
}
