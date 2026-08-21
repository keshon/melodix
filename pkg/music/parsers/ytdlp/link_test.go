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

// lastLines is what carries yt-dlp's own explanation into the log. Losing it is
// how "No supported JavaScript runtime could be found" stayed invisible behind
// a bare "exit status 1" for an hour of debugging.
func TestLastLines(t *testing.T) {
	const stderr = `[youtube] Extracting URL: https://www.youtube.com/watch?v=x
[youtube] x: Downloading webpage
WARNING: [youtube] No supported JavaScript runtime could be found.
ERROR: [youtube] x: Requested format is not available`

	got := lastLines(stderr, 2)
	if !strings.Contains(got, "Requested format is not available") {
		t.Fatalf("the final message must survive: %q", got)
	}
	if !strings.Contains(got, "JavaScript runtime") {
		t.Fatalf("the line before it is often the real cause: %q", got)
	}
	if strings.Contains(got, "Downloading webpage") {
		t.Fatalf("progress noise should be dropped: %q", got)
	}
	if strings.ContainsRune(got, '\n') {
		t.Fatalf("must stay one log-friendly line: %q", got)
	}

	if got := lastLines("   ", 2); got != "" {
		t.Fatalf("blank stderr = %q, want empty", got)
	}
	if got := lastLines("only one line", 2); got != "only one line" {
		t.Fatalf("short stderr = %q", got)
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
