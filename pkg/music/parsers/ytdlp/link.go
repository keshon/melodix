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
	ytdlp := exec.Command(YtdlpPath, "-j", "-f", audioFormatSelector, track.URL)
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
		IsLive      bool              `json:"is_live"`
	}

	var info ytdlpInfo
	if err := json.Unmarshal(output, &info); err != nil {
		return nil, nil, fmt.Errorf("ytdlp: decode json: %w", err)
	}

	// A live broadcast is HLS, and handing its playlist URL to ffmpeg does not
	// work: googlevideo answers ffmpeg's segment requests with 403 after roughly
	// twenty seconds, because the segment URLs are issued for the client that
	// asked for them and ffmpeg is not that client. Decline, so the chain reaches
	// the pipe parser, where yt-dlp fetches the segments itself and keeps the
	// identity that goes with them.
	if info.IsLive {
		return nil, nil, ErrLiveStream
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

	// yt-dlp reports the headers it used in http_headers so the fetch can be handed
	// off; passing the UA on keeps ffmpeg's request faithful to the one that
	// resolved the URL. Measured against googlevideo, the UA does not decide a 403
	// — the issuing InnerTube client does — so this is hygiene, not a fix.
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
