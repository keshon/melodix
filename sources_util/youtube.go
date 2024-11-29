package sources_util

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type YouTubeClient struct {
	BaseURL string
	Client  *http.Client
}

func NewYouTubeUtil() *YouTubeClient {
	return &YouTubeClient{
		BaseURL: "https://www.youtube.com",
		Client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

var (
	videoRegex          = regexp.MustCompile(`"url":"/watch\?v=([a-zA-Z0-9_-]+)(?:\\u0026list=([a-zA-Z0-9_-]+))?[^"]*`)
	mixPlaylistRegex    = regexp.MustCompile(`/watch\?v=([^&]+)&list=([^&]+)`)
	ErrNoVideoFound     = errors.New("no video found for the given title")
	ErrNoURLsInPlaylist = errors.New("no video URLs found in the playlist")
)

func (y *YouTubeClient) FetchVideoURLByTitle(title string) (string, error) {
	searchURL := fmt.Sprintf("%s/results?search_query=%v", y.BaseURL, url.QueryEscape(title))

	resp, err := y.Client.Get(searchURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP request failed with status code %v", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	matches := videoRegex.FindStringSubmatch(string(body))
	if len(matches) > 1 {
		videoID := matches[1]
		listID := matches[2]
		url := fmt.Sprintf("%s/watch?v=%s", y.BaseURL, videoID)
		if listID != "" {
			url += "&list=" + listID
		}
		return url, nil
	}

	return "", ErrNoVideoFound
}

func (y *YouTubeClient) FetchMixPlaylistVideoURLs(mixPlaylistURL string) ([]string, error) {
	resp, err := y.Client.Get(mixPlaylistURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP request failed with status code %v", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	bodyString := strings.ReplaceAll(string(body), `\u0026`, "&")
	matches := mixPlaylistRegex.FindAllStringSubmatch(bodyString, -1)

	var videoURLs []string
	for _, match := range matches {
		if len(match) >= 3 {
			videoURLs = append(videoURLs, fmt.Sprintf("%s/watch?v=%s&list=%s", y.BaseURL, match[1], match[2]))
		}
	}

	if len(videoURLs) == 0 {
		return nil, ErrNoURLsInPlaylist
	}

	return y.removeDuplicateURLs(videoURLs), nil
}

func (y *YouTubeClient) removeDuplicateURLs(urls []string) []string {
	uniqueURLs := make(map[string]struct{}, len(urls))
	var result []string
	for _, url := range urls {
		if _, exists := uniqueURLs[url]; !exists {
			uniqueURLs[url] = struct{}{}
			result = append(result, url)
		}
	}
	return result
}
