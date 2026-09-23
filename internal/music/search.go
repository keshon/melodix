package music

import (
	"errors"
	"fmt"

	"github.com/keshon/melodix/pkg/music/sources"
	"github.com/keshon/melodix/pkg/music/sources/youtube"
)

// ErrNotSearchable means the source has nothing to rank: radio, or a name no
// source answers to.
var ErrNotSearchable = errors.New("source cannot be searched")

// Search returns at most limit hits for query from source, in the source's own
// ranking. An empty source means YouTube.
func (s *Service) Search(source, query string, limit int) ([]sources.SearchResult, error) {
	var searcher sources.Searcher
	switch source {
	case "", sources.YouTube:
		searcher = s.youtube
	case sources.SoundCloud:
		searcher = s.soundcloud
	default:
		return nil, fmt.Errorf("%w: %s", ErrNotSearchable, source)
	}
	return searcher.Search(query, limit)
}

// HitURL turns a hit's ID back into a URL Add can resolve. A YouTube id
// rebuilds offline; a SoundCloud id has to be looked up.
func (s *Service) HitURL(source, id string) (string, error) {
	switch source {
	case "", sources.YouTube:
		return youtube.VideoURL(id), nil
	case sources.SoundCloud:
		return s.soundcloud.PermalinkByID(id)
	default:
		return "", fmt.Errorf("%w: %s", ErrNotSearchable, source)
	}
}
