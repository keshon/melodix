package youtube

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

func isURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func isYouTubeURL(input string) bool {
	youtubeRegex := regexp.MustCompile(`(?:https?:\/\/)?(?:www\.|music\.)?(youtube\.com|youtu\.be)\/\S+`)
	return youtubeRegex.MatchString(input)
}

func isYouTubeVideoURL(s string) bool {
	return strings.Contains(s, "youtube.com/watch?v=") ||
		strings.Contains(s, "music.youtube.com/watch?v=") ||
		strings.Contains(s, "youtu.be/")
}

func MoveToFront(list []string, item string) []string {
	if len(list) == 0 || item == "" {
		return list
	}
	if list[0] == item {
		return list
	}

	ordered := make([]string, 0, len(list))
	ordered = append(ordered, item)

	for _, v := range list {
		if v != item {
			ordered = append(ordered, v)
		}
	}
	return ordered
}

func CleanVideoURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw // fallback to original
	}

	host := u.Hostname()

	switch host {
	case "youtu.be":
		// Short URL: https://youtu.be/<id>?t=123
		vid := strings.Trim(u.Path, "/")
		if vid == "" {
			return raw
		}
		return fmt.Sprintf("https://youtu.be/%s", vid)

	case "www.youtube.com", "youtube.com", "music.youtube.com":
		// Standard URL: https://www.youtube.com/watch?v=<id>&other=params
		if u.Path == "/watch" {
			vid := u.Query().Get("v")
			if vid != "" {
				// Rebuild URL with only v= parameter
				return fmt.Sprintf("https://%s/watch?v=%s", host, vid)
			}
		}
		return raw

	default:
		return raw
	}
}
