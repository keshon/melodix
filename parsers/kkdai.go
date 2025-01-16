package parsers

import (
	"fmt"
	"net"
	"net/http"
	urlstd "net/url"
	"strings"
	"time"

	kkdai_youtube "github.com/kkdai/youtube/v2"
)

type KkdaiWrapper struct {
	client *kkdai_youtube.Client
}

func NewKkdaiWrapper() *KkdaiWrapper {
	client := &kkdai_youtube.Client{HTTPClient: &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:          10,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
		Timeout: 30 * time.Second,
	}}

	return &KkdaiWrapper{
		client: client,
	}
}

func (k *KkdaiWrapper) GetStreamURL(url string) (string, Meta, error) {
	id, err := k.extractYoutubeID(url)
	if err != nil {
		return "", Meta{}, err
	}

	song, err := k.client.GetVideo(id)
	if err != nil {
		if strings.Contains(err.Error(), "UNPLAYABLE") {
			return "", Meta{}, fmt.Errorf("YouTube video is unplayable, possibly due to `region restrictions` or other issues.\n\n¯\\_(ツ)_/¯")
		}
		return "", Meta{}, err
	}

	meta := Meta{
		ID:         song.ID,
		Title:      song.Title,
		WebPageURL: url,
		Duration:   song.Duration.Seconds(),
	}

	if len(song.Formats) == 0 {
		return "", Meta{}, fmt.Errorf("no formats found")
	}
	// format := song.Formats[0]

	// streamURL, err := k.client.GetStreamURL(song, &format)
	// if err != nil {
	// 	return "", Meta{}, err
	// }
	streamURL := song.Formats.WithAudioChannels()[0].URL

	return streamURL, meta, nil
}

func (s *KkdaiWrapper) extractYoutubeID(url string) (string, error) {
	parsedURL, err := urlstd.Parse(url)
	if err != nil {
		fmt.Println("Error parsing URL:", err)
		return "", err
	}
	queryParams := parsedURL.Query()

	videoID := queryParams.Get("v")
	if videoID != "" {
		return videoID, nil
	}

	fmt.Println("Video ID not found.")
	return "", nil
}

func (k *KkdaiWrapper) GetMetaInfo(url string) (Meta, error) {
	return Meta{}, nil
}

func (k *KkdaiWrapper) GetStream(url string) (string, error) {
	return "", nil
}
