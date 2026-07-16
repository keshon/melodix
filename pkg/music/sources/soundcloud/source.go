package soundcloud

import (
	"errors"
	"slices"
	"strings"

	source "github.com/keshon/melodix/pkg/music/sources"
)

// Name is this source's identifier (equals sources.SoundCloud).
const Name string = "soundcloud"

// Source resolves SoundCloud URLs and search queries.
type Source struct {
	searcher *Searcher
}

// New creates the SoundCloud source.
func New() *Source {
	return &Source{
		searcher: NewSearcher(),
	}
}

// Match claims soundcloud.com URLs only. Bare search queries are routed by the
// resolver (explicit source selection or the YouTube default), not by Match.
func (s *Source) Match(input string) bool {
	return strings.Contains(input, "soundcloud.com")
}

func (s *Source) Resolve(input string, selectedParser string) ([]source.TrackInfo, error) {
	parsers := s.AvailableParsers()

	if selectedParser == "" {
		if len(parsers) == 0 {
			return nil, errors.New(Name + " has no available parsers")
		}
		selectedParser = parsers[0]
	}

	if !slices.Contains(parsers, selectedParser) {
		return nil, errors.New(Name + " source does not support " + selectedParser + " parser")
	}

	input = strings.TrimSpace(input)

	// if it's a url, just return it as-is
	if source.IsURL(input) {
		return []source.TrackInfo{
			{
				URL:              input,
				Title:            "",
				SourceName:       Name,
				AvailableParsers: source.PreferParser(parsers, selectedParser),
			},
		}, nil
	}

	// otherwise, search by title
	trackURL, err := s.searcher.SearchFirstTrackURL(input)
	if err != nil {
		return nil, err
	}

	return []source.TrackInfo{
		{
			URL:              trackURL,
			Title:            input,
			SourceName:       Name,
			AvailableParsers: source.PreferParser(parsers, selectedParser),
		},
	}, nil
}

func (s *Source) SourceName() string {
	return Name
}

func (s *Source) AvailableParsers() []string {
	return []string{source.ParserScnativeLink, source.ParserYtdlpPipe, source.ParserYtdlpLink}
}
