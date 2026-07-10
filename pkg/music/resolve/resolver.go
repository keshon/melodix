// Package resolve resolves URLs and search queries to track metadata using configurable sources (YouTube, SoundCloud, radio).
package resolve

import (
	"errors"

	"github.com/keshon/melodix/pkg/music/sources"
	"github.com/keshon/melodix/pkg/music/sources/radio"
	"github.com/keshon/melodix/pkg/music/sources/soundcloud"
	"github.com/keshon/melodix/pkg/music/sources/youtube"
)

type Resolver struct {
	Sources map[string]sources.Source
}

func New() *Resolver {
	youtubeSource := youtube.New()
	soundcloudSource := soundcloud.New()
	radioSource := radio.New()

	return &Resolver{
		Sources: map[string]sources.Source{
			youtubeSource.SourceName():    youtubeSource,
			soundcloudSource.SourceName(): soundcloudSource,
			radioSource.SourceName():      radioSource,
		},
	}
}

func (r *Resolver) Resolve(input, selectedSource, selectedParser string) ([]sources.TrackInfo, error) {
	// Direct source selection
	if selectedSource != "" {
		src, ok := r.Sources[selectedSource]
		if !ok {
			return nil, errors.New("unknown source: " + selectedSource)
		}
		selectedParser, err := ensureParser(src, selectedParser)
		if err != nil {
			return nil, err
		}

		if !isURL(input) {
			if selectedSource != sources.YouTube && selectedSource != sources.SoundCloud {
				return nil, errors.New("title search is only supported on " + sources.YouTube + " and " + sources.SoundCloud)
			}
			return src.Resolve(input, selectedParser)
		}
		if !src.Match(input) {
			return nil, errors.New("input does not match selected source: " + selectedSource)
		}
		return src.Resolve(input, selectedParser)
	}

	// Automatic detection
	if !isURL(input) {
		yt, ok := r.Sources[sources.YouTube]
		if !ok {
			return nil, errors.New(youtube.Name + " source not available for title search")
		}
		selectedParser, err := ensureParser(yt, selectedParser)
		if err != nil {
			return nil, err
		}
		return yt.Resolve(input, selectedParser)
	}

	// Deterministic precedence for URL auto-detect (map iteration order is random);
	// radio stays the final fallback below. A new source must be added here as well
	// as in New().
	for _, typ := range []string{sources.YouTube, sources.SoundCloud} {
		s, ok := r.Sources[typ]
		if !ok {
			continue
		}
		if s.Match(input) {
			selectedParser, err := ensureParser(s, selectedParser)
			if err != nil {
				return nil, err
			}
			return s.Resolve(input, selectedParser)
		}
	}

	if radioSrc, ok := r.Sources[sources.Radio]; ok {
		selectedParser, err := ensureParser(radioSrc, selectedParser)
		if err != nil {
			return nil, err
		}
		return radioSrc.Resolve(input, selectedParser)
	}

	return nil, errors.New("no matching source found")
}

func ensureParser(src sources.Source, selected string) (string, error) {
	if selected != "" {
		return selected, nil
	}
	parsers := src.AvailableParsers()
	if len(parsers) == 0 {
		return "", errors.New("no parsers available for " + src.SourceName())
	}
	return parsers[0], nil
}
