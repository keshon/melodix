package yt_dlp

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/lrstanley/go-ytdlp"
)

type Ytdlp struct{}

// New creates a new instance of the Ytdlp struct.
func New() *Ytdlp {
	return &Ytdlp{}
}

// Download processes a URL and returns the last link from the output.
func (y *Ytdlp) GetStreamURL(url string) (string, error) {
	dl := ytdlp.New().GetURL()

	// Run the yt-dlp command and capture the result.
	result, err := dl.Run(context.TODO(), url)
	if err != nil {
		return "", fmt.Errorf("failed to execute yt-dlp: %w", err)
	}

	// Split the output into lines and extract the last valid URL.
	lines := strings.Split(result.Stdout, "\n")

	if len(lines) == 0 {
		return "", fmt.Errorf("no valid URL found in output")
	}

	lastLine := lines[len(lines)-1]

	re := regexp.MustCompile(`^https?://`)
	if !re.MatchString(lastLine) {
		return "", fmt.Errorf("last line is not a valid URL")
	}

	return lastLine, nil
}
