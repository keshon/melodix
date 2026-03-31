package radio

import (
	"errors"
	"slices"

	source "github.com/keshon/melodix/pkg/music/sources"
)

const Name = "radio"

type Source struct {
	resolver *Resolver
}

func New() *Source {
	return &Source{
		resolver: NewResolver(),
	}
}

func (r *Source) Match(input string) bool {
	ok, _, err := r.resolver.IsValidURL(input)
	return err == nil && ok
}

func (r *Source) Resolve(input string, selectedParser string) ([]source.TrackInfo, error) {
	parsers := r.AvailableParsers()

	if selectedParser == "" {
		if len(parsers) == 0 {
			return nil, errors.New(Name + " has no available parsers")
		}
		selectedParser = parsers[0]
	}

	if !slices.Contains(parsers, selectedParser) {
		return nil, errors.New(Name + " source does not support " + selectedParser + " parser")
	}

	ok, _, err := r.resolver.IsValidURL(input)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("invalid radio URL: " + input)
	}

	return []source.TrackInfo{
		{
			URL:              input,
			Title:            "", // maybe later via icy-* headers
			SourceName:       Name,
			AvailableParsers: MoveToFront(parsers, selectedParser),
		},
	}, nil
}

func (r *Source) SourceName() string {
	return Name
}

func (r *Source) AvailableParsers() []string {
	return []string{"ffmpeg-link"}
}
