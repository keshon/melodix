package soundcloud

import (
	"errors"
	"slices"
	"strings"

	source "github.com/keshon/melodix/internal/music/sources"
)

const SourceSoundCloud string = "soundcloud"

type SoundCloudSource struct {
	resolver *SoundCloudResolver
}

func New() *SoundCloudSource {
	return &SoundCloudSource{
		resolver: NewSoundCloudResolver(),
	}
}

func (s *SoundCloudSource) Match(input string) bool {
	return strings.Contains(input, "soundcloud.com") || !strings.HasPrefix(input, "http")
}

func (s *SoundCloudSource) Resolve(input string, selectedParser string) ([]source.TrackInfo, error) {
	parsers := s.AvailableParsers()

	if selectedParser == "" {
		if len(parsers) == 0 {
			return nil, errors.New(SourceSoundCloud + " has no available parsers")
		}
		selectedParser = parsers[0]
	}

	if !slices.Contains(parsers, selectedParser) {
		return nil, errors.New(SourceSoundCloud + " source does not support " + selectedParser + " parser")
	}

	input = strings.TrimSpace(input)

	// if it's a url, just return it as-is
	if isURL(input) {
		return []source.TrackInfo{
			{
				URL:              input,
				Title:            "",
				SourceName:       SourceSoundCloud,
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
			SourceName:       SourceSoundCloud,
			AvailableParsers: MoveToFront(parsers, selectedParser),
		},
	}, nil
}

func (s *SoundCloudSource) SourceName() string {
	return SourceSoundCloud
}

func (s *SoundCloudSource) AvailableParsers() []string {
	return []string{"ytdlp-pipe", "ytdlp-link"}
}
