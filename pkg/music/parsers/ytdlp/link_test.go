package ytdlp

import (
	"encoding/json"
	"strings"
	"testing"
)

// yt-dlp's http_headers casing is not contractual, and a nil map is normal for
// formats that need no headers -- neither may panic or silently drop the UA.
func TestHeaderValue(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		want    string
	}{
		{"exact", map[string]string{"User-Agent": "ua"}, "ua"},
		{"lowercase", map[string]string{"user-agent": "ua"}, "ua"},
		{"screaming", map[string]string{"USER-AGENT": "ua"}, "ua"},
		{"absent", map[string]string{"Accept": "*/*"}, ""},
		{"nil map", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := headerValue(tt.headers, "User-Agent"); got != tt.want {
				t.Fatalf("headerValue = %q, want %q", got, tt.want)
			}
		})
	}
}

// Guards the json tags against a yt-dlp output change: the UA has to survive the
// decode, or ffmpeg silently goes back to asking googlevideo as Lavf/… and 403s.
func TestYtdlpInfo_DecodesHTTPHeaders(t *testing.T) {
	const payload = `{
		"duration": 244.0,
		"url": "https://rr3---sn-x.googlevideo.com/videoplayback?n=abc",
		"http_headers": {"User-Agent": "Mozilla/5.0 (probe)", "Accept": "*/*"},
		"formats": [
			{"url": "https://fallback.example/a", "http_headers": {"User-Agent": "fallback-ua"}}
		]
	}`

	// Mirrors the anonymous structs decoded in ytdlpLink.
	var info struct {
		Duration    float64           `json:"duration"`
		URL         string            `json:"url"`
		HTTPHeaders map[string]string `json:"http_headers"`
		Formats     []struct {
			URL         string            `json:"url"`
			HTTPHeaders map[string]string `json:"http_headers"`
		} `json:"formats"`
	}
	if err := json.Unmarshal([]byte(payload), &info); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got := headerValue(info.HTTPHeaders, "User-Agent"); got != "Mozilla/5.0 (probe)" {
		t.Fatalf("top-level UA = %q", got)
	}
	if len(info.Formats) != 1 {
		t.Fatalf("formats = %d, want 1", len(info.Formats))
	}
	if got := headerValue(info.Formats[0].HTTPHeaders, "User-Agent"); got != "fallback-ua" {
		t.Fatalf("format UA = %q", got)
	}
	if !strings.Contains(info.URL, "googlevideo") {
		t.Fatalf("url = %q", info.URL)
	}
}

// The is_live tag decides whether link mode declines, so a yt-dlp output change
// here would silently put live streams back on the path that 403s after twenty
// seconds. Mirrors the anonymous struct decoded in ytdlpLink.
func TestYtdlpInfo_DecodesIsLive(t *testing.T) {
	for _, tt := range []struct {
		name    string
		payload string
		want    bool
	}{
		{"live", `{"is_live": true, "url": "https://x/y"}`, true},
		{"vod", `{"is_live": false, "duration": 244.0, "url": "https://x/y"}`, false},
		{"absent", `{"duration": 244.0, "url": "https://x/y"}`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var info struct {
				IsLive bool `json:"is_live"`
			}
			if err := json.Unmarshal([]byte(tt.payload), &info); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if info.IsLive != tt.want {
				t.Fatalf("IsLive = %v, want %v", info.IsLive, tt.want)
			}
		})
	}
}

// The muxed fallback only ever applies where no audio-only format exists, and
// it is capped so a live stream is not fetched at 5.5 Mbit/s to be thrown away.
func TestAudioFormatSelectorShape(t *testing.T) {
	if !strings.HasPrefix(audioFormatSelector, "bestaudio/") {
		t.Fatalf("selector must prefer audio-only first: %q", audioFormatSelector)
	}
	if !strings.Contains(audioFormatSelector, "height<=") {
		t.Fatalf("the muxed fallback must be capped, or live costs megabits: %q", audioFormatSelector)
	}
}
