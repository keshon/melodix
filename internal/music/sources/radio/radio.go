package radio

import (
	"errors"
	"slices"

	source "github.com/keshon/melodix/internal/music/sources"
)

const SourceRadio = "radio"

type RadioSource struct {
	resolver *RadioResolver
}

func New() *RadioSource {
	return &RadioSource{
		resolver: NewRadioResolver(),
	}
}

func (r *RadioSource) Match(input string) bool {
	ok, _, err := r.resolver.IsValidURL(input)
	return err == nil && ok
}

func (r *RadioSource) Resolve(input string, selectedParser string) ([]source.TrackInfo, error) {
	parsers := r.AvailableParsers()

	if selectedParser == "" {
		if len(parsers) == 0 {
			return nil, errors.New(SourceRadio + " has no available parsers")
		}
		selectedParser = parsers[0]
	}

	if !slices.Contains(parsers, selectedParser) {
		return nil, errors.New(SourceRadio + " source does not support " + selectedParser + " parser")
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
			SourceName:       SourceRadio,
			AvailableParsers: MoveToFront(parsers, selectedParser),
		},
	}, nil
}

func (r *RadioSource) SourceName() string {
	return SourceRadio
}

func (r *RadioSource) AvailableParsers() []string {
	return []string{"ffmpeg-link"}
}
