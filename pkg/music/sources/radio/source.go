package radio

import (
	"errors"
	"slices"

	source "github.com/keshon/melodix/pkg/music/sources"
)

// Name is this source's identifier (equals sources.Radio).
const Name = "radio"

// Source plays internet radio streams (validated by probing Content-Type).
type Source struct {
	validator *Validator
}

// New creates the radio source.
func New() *Source {
	return &Source{
		validator: NewValidator(),
	}
}

func (r *Source) Match(input string) bool {
	ok, _, err := r.validator.IsValidURL(input)
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

	ok, _, err := r.validator.IsValidURL(input)
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
			AvailableParsers: source.PreferParser(parsers, selectedParser),
		},
	}, nil
}

func (r *Source) SourceName() string {
	return Name
}

func (r *Source) AvailableParsers() []string {
	return []string{source.ParserFFmpegLink}
}
