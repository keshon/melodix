package parsers

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

func NewYtdlpWrapper() *YtdlpWrapper {
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

	meta.Parser = "yt-dlp"
	return meta, nil
}

func (y *YtdlpWrapper) GetStream(url string) (*ytdlp.Result, string, error) {
	cacheDir := "./cache"

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, "", fmt.Errorf("failed to create cache directory: %w", err)
	}

	timestamp := time.Now().Format("20060102_150405")
	outputFile := filepath.Join(cacheDir, timestamp+".webm")

	dl := ytdlp.New().
		FormatSort("res,ext:webm:webm").
		NoPart().
		NoPlaylist().
		NoOverwrites().
		Output(outputFile)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	result, err := dl.Run(ctx, url)
	if err != nil {
		_ = os.Remove(outputFile)
		return nil, "", fmt.Errorf("failed to download stream from URL %q: %w", url, err)
	}

	absPath, err := filepath.Abs(outputFile)
	if err != nil {
		return nil, "", fmt.Errorf("failed to resolve absolute path for %q: %w", outputFile, err)
	}

	return result, absPath, nil
}
