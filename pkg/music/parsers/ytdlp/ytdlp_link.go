package ytdlp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/keshon/melodix/pkg/music/parsers"
)

func ytdlpLink(track *parsers.TrackParse, seekSec float64) (io.ReadCloser, func(), error) {
	ytdlp := exec.Command("yt-dlp", "-j", "-f", "bestaudio", track.URL)
	output, err := ytdlp.Output()
	if err != nil {
		return nil, nil, fmt.Errorf("yt-dlp get-url error: %w", err)
	}

	type fragment struct {
		Duration float64 `json:"duration"`
	}

	type format struct {
		URL       string     `json:"url"`
		Fragments []fragment `json:"fragments,omitempty"`
	}

	type ytdlpInfo struct {
		Duration float64  `json:"duration"`
		Formats  []format `json:"formats"`
		URL      string   `json:"url"`
	}

	var info ytdlpInfo
	if err := json.Unmarshal(output, &info); err != nil {
		return nil, nil, fmt.Errorf("json unmarshal error: %w", err)
	}

	// If the root duration is empty, we try to take it from the first fragment of the first format
	if info.Duration == 0 && len(info.Formats) > 0 {
		if len(info.Formats[0].Fragments) > 0 {
			info.Duration = info.Formats[0].Fragments[0].Duration
		}
	}

	link := strings.TrimSpace(info.URL)
	if link == "" && len(info.Formats) > 0 {
		link = strings.TrimSpace(info.Formats[0].URL)
	}
	if link == "" {
		return nil, nil, errors.New("empty URL returned from yt-dlp")
	}

	track.Duration = time.Duration(info.Duration * float64(time.Second))

	ffmpeg := exec.Command("ffmpeg",
		"-ss", fmt.Sprintf("%.3f", seekSec),
		"-reconnect", "1",
		"-reconnect_streamed", "1",
		"-reconnect_delay_max", "5",
		"-i", link,
		"-f", "s16le",
		"-ar", fmt.Sprintf("%d", sampleRate),
		"-ac", fmt.Sprintf("%d", channels),
		"-loglevel", "warning",
		"pipe:1",
	)

	reader, err := ffmpeg.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("stdout pipe error: %w", err)
	}

	if err := ffmpeg.Start(); err != nil {
		return nil, nil, fmt.Errorf("command start error: %w", err)
	}

	cleanup := func() {
		ffmpeg.Process.Kill()
	}

	return reader, cleanup, nil
}
