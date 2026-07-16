package ytdlp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/keshon/melodix/pkg/music/opus"
	"github.com/keshon/melodix/pkg/music/parsers"
	ffmpegparser "github.com/keshon/melodix/pkg/music/parsers/ffmpeg"
)

func ytdlpLink(track *parsers.Track, seekSec float64) (opus.Reader, func(), error) {
	ytdlp := exec.Command(YtdlpPath, "-j", "-f", "bestaudio", track.URL)
	output, err := ytdlp.Output()
	if err != nil {
		return nil, nil, fmt.Errorf("ytdlp: get url: %w", err)
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
		return nil, nil, fmt.Errorf("ytdlp: decode json: %w", err)
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
		return nil, nil, errors.New("ytdlp: empty url returned")
	}

	track.Duration = time.Duration(info.Duration * float64(time.Second))

	return ffmpegparser.OpusReader(ffmpegparser.NewPCMCommand(link, seekSec, true, "ytdlp-link"), "ytdlp")
}
