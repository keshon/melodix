package yt_dlp

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

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

	result, err := dl.Run(context.TODO(), url)
	if err != nil {
		return "", fmt.Errorf("failed to execute yt-dlp: %w", err)
	}

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

type Meta struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	WebPageURL string  `json:"webpage_url"`
	Duration   float64 `json:"duration"`
}

func (y *Ytdlp) GetMetaInfo(url string) (Meta, error) {
	timestamp := time.Now().Format("20060102_150405")
	dl := ytdlp.New().DumpJSON().SkipDownload().Output(timestamp + ".%(ext)s")
	result, err := dl.Run(context.TODO(), url)
	if err != nil {
		return Meta{}, fmt.Errorf("failed to execute yt-dlp: %w", err)
	}

	var meta Meta
	byteResult := []byte(result.Stdout)

	if err := json.Unmarshal(byteResult, &meta); err != nil {
		return Meta{}, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	return meta, nil
}
