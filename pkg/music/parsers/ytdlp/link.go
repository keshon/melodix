package ytdlp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/keshon/melodix/pkg/music/parsers"
	ffmpegparser "github.com/keshon/melodix/pkg/music/parsers/ffmpeg"
)

func ytdlpLink(track parsers.Track, seekSec float64) (parsers.Opened, error) {
	output, err := runJSON(exec.Command(YtdlpPath, args("-j", "-f", audioFormatSelector, track.URL)...), "get url")
	if err != nil {
		return parsers.Opened{}, err
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
		return parsers.Opened{}, fmt.Errorf("ytdlp: decode json: %w", err)
	}

	// If the root duration is empty, we try to take it from the first fragment of
	// the first format
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
		return parsers.Opened{}, errors.New("ytdlp: empty url returned")
	}

	// yt-dlp reports the headers it used in http_headers so the fetch can be
	// handed off; passing the UA on keeps ffmpeg's request faithful to the one
	// that resolved the URL. Measured against googlevideo, the UA does not decide
	// a 403 — the issuing InnerTube client does — so this is hygiene, not a fix.
	cmd := ffmpegparser.NewPCMCommandUA(link, seekSec, true, "ytdlp-link", headerValue(headers, "User-Agent"))
	r, cleanup, err := ffmpegparser.OpusReader(cmd, "ytdlp")
	if err != nil {
		return parsers.Opened{}, err
	}
	return parsers.Opened{
		Reader:   r,
		Cleanup:  cleanup,
		Duration: time.Duration(info.Duration * float64(time.Second)),
	}, nil
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
