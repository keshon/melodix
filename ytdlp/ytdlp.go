package ytdlp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/lrstanley/go-ytdlp"
)

type YtdlpWrapper struct{}

func New() *YtdlpWrapper {
	return &YtdlpWrapper{}
}

func (y *YtdlpWrapper) GetStreamURL(url string) (string, error) {
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

func (y *YtdlpWrapper) GetMetaInfo(url string) (Meta, error) {
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

func (y *YtdlpWrapper) GetStream(url string) (string, error) {
	cacheDir := "./cache"

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create cache directory: %w", err)
	}

	timestamp := time.Now().Format("20060102_150405")
	outputFile := filepath.Join(cacheDir, timestamp+".webm")

	dl := ytdlp.New().Output(outputFile)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if _, err := dl.Run(ctx, url); err != nil {
		_ = os.Remove(outputFile)
		return "", fmt.Errorf("failed to download stream from URL %q: %w", url, err)
	}

	absPath, err := filepath.Abs(outputFile)
	if err != nil {
		return "", fmt.Errorf("failed to resolve absolute path for %q: %w", outputFile, err)
	}

	return absPath, nil
}