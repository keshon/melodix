package youtube

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"
)

var (
	watchURLPattern  = regexp.MustCompile(`"url":"/watch\?v=([a-zA-Z0-9_-]{11})`)
	ErrNoVideoMatch  = errors.New("no video found for the given title")
	ErrEmptyPlaylist = errors.New("no video URLs found in the playlist")
)

type YouTubeResolver struct {
	BaseURL string
	Client  *http.Client
}

func NewYouTubeResolver() *YouTubeResolver {
	return &YouTubeResolver{
		BaseURL: "https://www.youtube.com",
		Client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (r *YouTubeResolver) SearchFirstVideoURL(query string) (string, error) {
	searchURL := fmt.Sprintf("%s/results?search_query=%s", r.BaseURL, url.QueryEscape(query))

	resp, err := r.Client.Get(searchURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("YouTube search failed with status code %v", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Only match video IDs without playlist
	matches := watchURLPattern.FindAllStringSubmatch(string(body), -1)
	if len(matches) == 0 {
		return "", ErrNoVideoMatch
	}

	videoID := matches[0][1] // first match only
	resultURL := fmt.Sprintf("%s/watch?v=%s", r.BaseURL, videoID)
	return resultURL, nil
}
