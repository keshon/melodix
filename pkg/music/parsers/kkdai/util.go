package kkdai

import (
	"errors"
	"strings"
)

func extractYouTubeID(url string) (string, error) {
	switch {
	case strings.Contains(url, "youtu.be/"):
		parts := strings.Split(url, "youtu.be/")
		if len(parts) != 2 {
			return "", errors.New("invalid YouTube URL format")
		}
		return strings.Split(parts[1], "?")[0], nil

	case strings.Contains(url, "youtube.com/watch?v="):
		parts := strings.Split(url, "v=")
		if len(parts) != 2 {
			return "", errors.New("invalid YouTube URL format")
		}
		return strings.Split(parts[1], "&")[0], nil

	default:
		return "", errors.New("unsupported URL format")
	}
}
