package soundcloud

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
	trackLinkRegex  = regexp.MustCompile(`(?s)<a class="result__url"[^>]*>\s*(soundcloud\.com/[^<]+)\s*</a>`)
	ErrNoTrackMatch = errors.New("no track found for the given query")
)

type SoundCloudResolver struct {
	BaseURL string
	Client  *http.Client
}

func NewSoundCloudResolver() *SoundCloudResolver {
	return &SoundCloudResolver{
		BaseURL: "https://soundcloud.com",
		Client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (r *SoundCloudResolver) SearchFirstTrackURL(query string) (string, error) {
	searchURL := fmt.Sprintf("https://duckduckgo.com/html/?q=site:soundcloud.com+%s", url.QueryEscape(query))

	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := r.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("DuckDuckGo search failed with status code %v", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	matches := trackLinkRegex.FindStringSubmatch(string(body))
	if len(matches) < 2 {
		return "", ErrNoTrackMatch
	}

	trackURL := "https://" + matches[1]
	return trackURL, nil
}
