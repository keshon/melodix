package youtube

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

func isYouTubeURL(input string) bool {
	youtubeRegex := regexp.MustCompile(`(?:https?:\/\/)?(?:www\.|music\.)?(youtube\.com|youtu\.be)\/\S+`)
	return youtubeRegex.MatchString(input)
}

// ExtractListID returns the list id from any YouTube URL, or "" when there is
// none. It does not judge what to do with it — see shouldExpandList.
func ExtractListID(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return u.Query().Get("list")
}

// ExtractVideoID returns the watch video id from a YouTube URL, or "".
func ExtractVideoID(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	if strings.EqualFold(u.Hostname(), "youtu.be") {
		return strings.Trim(u.Path, "/")
	}
	return u.Query().Get("v")
}

// shouldExpandList decides whether a URL means "play this list".
//
// Any link carrying a list id does, which is what YouTube itself means by one:
// opening watch?v=X&list=L plays X and then continues through L. The video is
// not ignored — it becomes the first track (see seedFirst) — so expanding costs
// the caller nothing, while treating the list as decoration would throw away
// the part of the link that is harder to reconstruct. To play a single video,
// link it without the list.
func shouldExpandList(listID string) bool {
	return listID != ""
}

func isYouTubeVideoURL(s string) bool {
	return strings.Contains(s, "youtube.com/watch?v=") ||
		strings.Contains(s, "music.youtube.com/watch?v=") ||
		strings.Contains(s, "youtu.be/")
}

// CleanVideoURL strips tracking/playlist params, returning a canonical watch URL.
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
