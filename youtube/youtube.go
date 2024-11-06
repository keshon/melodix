package youtube

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
)

type YouTube struct {
	URL string
}

func New() *YouTube {
	return &YouTube{}
}

func (y *YouTube) GetVideoURLByTitle(title string) (string, error) {
	searchURL := fmt.Sprintf("https://www.youtube.com/results?search_query=%v", strings.ReplaceAll(title, " ", "+"))

	resp, err := http.Get(searchURL)
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

	re := regexp.MustCompile(`"url":"/watch\?v=([a-zA-Z0-9_-]+)(?:\\u0026list=([a-zA-Z0-9_-]+))?[^"]*`)
	matches := re.FindAllStringSubmatch(string(body), -1)

	if len(matches) > 0 && len(matches[0]) > 1 {
		videoID := matches[0][1]
		listID := matches[0][2]

		url := "https://www.youtube.com/watch?v=" + videoID
		if listID != "" {
			url += "&list=" + listID
		}

		slog.Info(url)

		return url, nil
	}

	return "", fmt.Errorf("no video found for the given title")
}

func (y *YouTube) GetVdeoURLByVideoLink(url string) string {
	return ""
}

func (y *YouTube) GetVideoURLByPlaylistLink(url string) string {
	return ""
}

func (y *YouTube) parsePlaylistID(url string) string {
	if strings.Contains(url, "list=") {
		splitURL := strings.Split(url, "list=")
		if len(splitURL) > 1 {
			return splitURL[1]
		}
	}
	return ""
}

func (y *YouTube) parseVideoID(url string) string {
	re := regexp.MustCompile(`watch\?v=([^&]+)&list=`)

	match := re.FindStringSubmatch(url)
	if len(match) >= 2 {
		return match[1]
	}

	return ""
}
