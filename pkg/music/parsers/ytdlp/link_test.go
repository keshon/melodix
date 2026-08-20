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
