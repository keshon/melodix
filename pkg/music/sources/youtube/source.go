package youtube

import (
	"errors"
	"slices"
	"strings"

	source "github.com/keshon/melodix/pkg/music/sources"
)

// Name is this source's identifier (equals sources.YouTube).
const Name string = "youtube"

// Source resolves YouTube URLs and search queries.
type Source struct {
	searcher  *Searcher
	playlists *PlaylistFetcher
}

// New creates the YouTube source.
func New() *Source {
	return &Source{
		searcher:  NewSearcher(),
		playlists: NewPlaylistFetcher(),
	}
}

func (y *Source) Match(input string) bool {
	return isYouTubeURL(input)
}

func (y *Source) Resolve(input string, selectedParser string) ([]source.TrackInfo, error) {
	parsers := y.AvailableParsers()

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
	preferred := source.PreferParser(parsers, selectedParser)

	// playlist or mix: one link expands to many tracks
	if listID := ExtractListID(input); shouldExpandList(input, listID) {
		result, err := y.playlists.Fetch(listID, ExtractVideoID(input))
		if err != nil {
			return nil, err
		}
		tracks := make([]source.TrackInfo, 0, len(result.Entries))
		for _, e := range result.Entries {
			tracks = append(tracks, source.TrackInfo{
				URL:              "https://www.youtube.com/watch?v=" + e.VideoID,
				Title:            e.Title,
				SourceName:       Name,
				AvailableParsers: preferred,
			})
		}
		return tracks, nil
	}

	// direct video URL
	if isYouTubeVideoURL(input) {
		input = CleanVideoURL(input)
		return []source.TrackInfo{
			{
				URL:              input,
				Title:            "",
				SourceName:       Name,
				AvailableParsers: source.PreferParser(parsers, selectedParser),
			},
		}, nil
	}

	if source.IsURL(input) {
		return nil, errors.New("invalid YouTube URL format")
	}

	// by title
	videoURL, err := y.searcher.SearchFirstVideoURL(input)
	if err != nil {
		return nil, errors.New("could not find YouTube video for query")
	}

	return []source.TrackInfo{
		{
			URL:              videoURL,
			Title:            input,
			SourceName:       Name,
			AvailableParsers: source.PreferParser(parsers, selectedParser),
		},
	}, nil
}

func (y *Source) SourceName() string {
	return Name
}

func (y *Source) AvailableParsers() []string {
	// Passthrough paths first (ytnative, then kkdai-pipe — no ffmpeg), then the
	// ffmpeg-encode fallbacks (kkdai-link, yt-dlp).
	return []string{
		source.ParserYtnativeLink,
		source.ParserKkdaiPipe,
		source.ParserKkdaiLink,
		source.ParserYtdlpLink,
		source.ParserYtdlpPipe,
	}
}
