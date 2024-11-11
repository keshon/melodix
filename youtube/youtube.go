package youtube

import (
	"fmt"
	"io"
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

		return url, nil
	}

	return "", fmt.Errorf("no video found for the given title")
}

// func (y *YouTube) GetVdeoURLByVideoLink(url string) string {
// 	return ""
// }

// func (y *YouTube) GetVideoURLByPlaylistLink(url string) string {
// 	return ""
// }

func (y *YouTube) GetVideoURLByMixPlaylistLink(url string) ([]string, error) {
	resp, err := http.Get(url)
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

	re := regexp.MustCompile(`/watch\?v=([^&]+)&list=([^&]+)`)

	matches := re.FindAllStringSubmatch(bodyString, -1)

	var videoURLs []string
	for _, match := range matches {
		if len(match) >= 3 {
			videoURL := fmt.Sprintf("https://www.youtube.com/watch?v=%s&list=%s", match[1], match[2])
			videoURLs = append(videoURLs, videoURL)
		}
	}

	if len(videoURLs) == 0 {
		return nil, fmt.Errorf("no video URLs found in the playlist")
	}

	videoURLs = y.removeDuplicatesFromStringSlice(videoURLs)

	return videoURLs, nil

	// var videoIDs []string
	// for _, videoURL := range videoURLs {
	// 	videoIDs = append(videoIDs, y.parseVideoID(videoURL))
	// }

	// return videoIDs, nil
}

func (y *YouTube) parseVideoID(url string) string {
	re := regexp.MustCompile(`watch\?v=([^&]+)&list=`)

	match := re.FindStringSubmatch(url)
	if len(match) >= 2 {
		return match[1]
	}

	return ""
}

func (y *YouTube) removeDuplicatesFromStringSlice(strSlice []string) []string {
	allKeys := make(map[string]bool)
	list := []string{}
	for _, item := range strSlice {
		if _, value := allKeys[item]; !value {
			allKeys[item] = true
			list = append(list, item)
		}
	}
	return list
}
