package youtube

import (
	"regexp"
	"strings"
)

type YouTube struct {
	URL string
}

func New() *YouTube {
	return &YouTube{}
}

func (y *YouTube) GetVideoURLByTitle(title string) string {
	return ""
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
