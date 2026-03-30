// Package resolver resolves URLs and search queries to track metadata using configurable sources (YouTube, SoundCloud, radio).
package resolver

import (
	"context"
	"errors"

	"github.com/keshon/melodix/pkg/music/sources"
	"github.com/keshon/melodix/pkg/music/sources/radio"
	"github.com/keshon/melodix/pkg/music/sources/soundcloud"
	"github.com/keshon/melodix/pkg/music/sources/youtube"
)

type SourceResolver struct {
	Sources map[string]sources.Source
}

func New() *SourceResolver {
	youtubeSource := youtube.New()
	soundcloudSource := soundcloud.New()
	radioSource := radio.New()

	return &SourceResolver{
		Sources: map[string]sources.Source{
			youtubeSource.SourceName():    youtubeSource,
			soundcloudSource.SourceName(): soundcloudSource,
			radioSource.SourceName():      radioSource,
		},
	}
}

func (r *SourceResolver) Resolve(ctx context.Context, input, selectedSource, selectedParser string) ([]sources.TrackInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
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
			if selectedSource != sources.SourceYouTube && selectedSource != sources.SourceSoundCloud {
				return nil, errors.New("title search is only supported on " + sources.SourceYouTube + " and " + sources.SourceSoundCloud)
			}
			return src.Resolve(ctx, input, selectedParser)
		}
		if !src.Match(input) {
			return nil, errors.New("input does not match selected source: " + selectedSource)
		}
		return src.Resolve(ctx, input, selectedParser)
	}

	// Automatic detection
	if !isURL(input) {
		yt, ok := r.Sources[sources.SourceYouTube]
		if !ok {
			return nil, errors.New(youtube.SourceYouTube + " source not available for title search")
		}
		selectedParser, err := ensureParser(yt, selectedParser)
		if err != nil {
			return nil, err
		}
		return yt.Resolve(ctx, input, selectedParser)
	}

	for typ, s := range r.Sources {
		if typ == sources.SourceRadio {
			continue
		}
		if s.Match(input) {
			selectedParser, err := ensureParser(s, selectedParser)
			if err != nil {
				return nil, err
			}
			return s.Resolve(ctx, input, selectedParser)
		}
	}

	if radioSrc, ok := r.Sources[sources.SourceRadio]; ok {
		selectedParser, err := ensureParser(radioSrc, selectedParser)
		if err != nil {
			return nil, err
		}
		return radioSrc.Resolve(ctx, input, selectedParser)
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
