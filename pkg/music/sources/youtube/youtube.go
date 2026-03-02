package youtube

import (
	"errors"
	"slices"
	"strings"

	source "github.com/keshon/melodix/pkg/music/sources"
)

const SourceYouTube string = "youtube"

type YouTubeSource struct {
	resolver *YouTubeResolver
}

func New() *YouTubeSource {
	return &YouTubeSource{
		resolver: NewYouTubeResolver(),
	}
}

func (y *YouTubeSource) Match(input string) bool {
	return isYouTubeURL(input)
}

func (y *YouTubeSource) Resolve(input string, selectedParser string) ([]source.TrackInfo, error) {
	parsers := y.AvailableParsers()

	if selectedParser == "" {
		if len(parsers) == 0 {
			return nil, errors.New(SourceYouTube + " has no available parsers")
		}
		selectedParser = parsers[0]
	}

	if !slices.Contains(parsers, selectedParser) {
		return nil, errors.New(SourceYouTube + " source does not support " + selectedParser + " parser")
	}

	input = strings.TrimSpace(input)

	// direct video URL
	if isYouTubeVideoURL(input) {
		input = CleanVideoURL(input)
		return []source.TrackInfo{
			{
				URL:              input,
				Title:            "",
				SourceName:       SourceYouTube,
				AvailableParsers: MoveToFront(parsers, selectedParser),
			},
		}, nil
	}

	if isURL(input) {
		return nil, errors.New("invalid YouTube URL format")
	}

	// by title
	videoURL, err := y.resolver.SearchFirstVideoURL(input)
	if err != nil {
		return nil, errors.New("could not find YouTube video for query")
	}

	return []source.TrackInfo{
		{
			URL:              videoURL,
			Title:            input,
			SourceName:       SourceYouTube,
			AvailableParsers: MoveToFront(parsers, selectedParser),
		},
	}, nil
}

func (y *YouTubeSource) SourceName() string {
	return SourceYouTube
}

func (y *YouTubeSource) AvailableParsers() []string {
	return []string{"kkdai-link", "kkdai-pipe", "ytdlp-link", "ytdlp-pipe"}
}
