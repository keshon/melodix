package kkdai

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
)

func getProxyList() []string {
	file, err := os.Open("proxies.txt")
	if err != nil {
		fmt.Printf("Failed to open proxy file: %v\n", err)
		return nil
	}
	defer file.Close()

	var proxies []string
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "#", 2)
		proxy := strings.TrimSpace(parts[0])
		if proxy != "" {
			proxies = append(proxies, proxy)
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Printf("Error reading proxy file: %v\n", err)
	}

	return proxies
}

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
