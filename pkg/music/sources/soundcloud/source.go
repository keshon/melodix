package soundcloud

import (
	"errors"
	"slices"
	"strings"

	source "github.com/keshon/melodix/pkg/music/sources"
)

const Name string = "soundcloud"

type Source struct {
	resolver *Resolver
}

func New() *Source {
	return &Source{
		resolver: NewResolver(),
	}
}

func (s *Source) Match(input string) bool {
	return strings.Contains(input, "soundcloud.com") || !strings.HasPrefix(input, "http")
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
	if isURL(input) {
		return []source.TrackInfo{
			{
				URL:              input,
				Title:            "",
				SourceName:       Name,
				AvailableParsers: MoveToFront(parsers, selectedParser),
			},
		}, nil
	}

	// otherwise, search by title
	trackURL, err := s.resolver.SearchFirstTrackURL(input)
	if err != nil {
		return nil, err
	}

	return []source.TrackInfo{
		{
			URL:              trackURL,
			Title:            input,
			SourceName:       Name,
			AvailableParsers: MoveToFront(parsers, selectedParser),
		},
	}, nil
}

func (s *Source) SourceName() string {
	return Name
}

func (s *Source) AvailableParsers() []string {
	return []string{"ytdlp-pipe", "ytdlp-link"}
}
