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
		URL         string            `json:"url"`
		Fragments   []fragment        `json:"fragments,omitempty"`
		HTTPHeaders map[string]string `json:"http_headers,omitempty"`
	}

	type ytdlpInfo struct {
		Duration    float64           `json:"duration"`
		Formats     []format          `json:"formats"`
		URL         string            `json:"url"`
		HTTPHeaders map[string]string `json:"http_headers,omitempty"`
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
	headers := info.HTTPHeaders
	if link == "" && len(info.Formats) > 0 {
		link = strings.TrimSpace(info.Formats[0].URL)
		headers = info.Formats[0].HTTPHeaders
	}
	if link == "" {
		return nil, nil, errors.New("ytdlp: empty url returned")
	}

	track.Duration = time.Duration(info.Duration * float64(time.Second))

	// googlevideo binds a stream URL to the client that obtained it. yt-dlp reports
	// the headers it used in http_headers precisely so the fetch can be handed off;
	// without them ffmpeg asks as Lavf/… and the CDN answers 403 on the first read.
	cmd := ffmpegparser.NewPCMCommandUA(link, seekSec, true, "ytdlp-link", headerValue(headers, "User-Agent"))
	return ffmpegparser.OpusReader(cmd, "ytdlp")
}

// headerValue looks up an HTTP header case-insensitively, returning "" when the
// map is nil or the header is absent. yt-dlp's http_headers casing is not
// contractual, so don't index it directly.
func headerValue(h map[string]string, name string) string {
	for k, v := range h {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return ""
}
