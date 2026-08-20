package ytnative

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchPlayerSendsClientContext(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		fmt.Fprint(w, `{
			"playabilityStatus": {"status": "OK"},
			"videoDetails": {"title": "A Song", "lengthSeconds": "215"},
			"streamingData": {"adaptiveFormats": [
				{"url": "https://cdn/audio", "mimeType": "audio/webm; codecs=\"opus\"", "bitrate": 130000}
			]}
		}`)
	}))
	defer srv.Close()

	pr, err := fetchPlayer(srv.Client(), srv.URL, "dQw4w9WgXcQ")
	if err != nil {
		t.Fatalf("fetchPlayer: %v", err)
	}
	if pr.VideoDetails.Title != "A Song" {
		t.Fatalf("title = %q", pr.VideoDetails.Title)
	}

	if gotBody["videoId"] != "dQw4w9WgXcQ" {
		t.Fatalf("videoId in request = %v", gotBody["videoId"])
	}
	client := gotBody["context"].(map[string]any)["client"].(map[string]any)
	// VISIONOS specifically: ANDROID_VR URLs 403 on the open-ended requests both
	// the passthrough and ffmpeg make. See the note on clientName.
	if client["clientName"] != "VISIONOS" {
		t.Fatalf("clientName = %v", client["clientName"])
	}
	if client["clientVersion"] != clientVersion {
		t.Fatalf("clientVersion = %v", client["clientVersion"])
	}
	if _, ok := client["androidSdkVersion"]; ok {
		t.Fatal("androidSdkVersion must not be sent for a non-Android client")
	}
}

func TestFetchPlayerNotPlayable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"playabilityStatus": {"status": "LOGIN_REQUIRED", "reason": "Sign in to confirm your age"}}`)
	}))
	defer srv.Close()

	_, err := fetchPlayer(srv.Client(), srv.URL, "abcdefghijk")
	if !errors.Is(err, ErrNotPlayable) {
		t.Fatalf("err = %v, want ErrNotPlayable", err)
	}
}

func TestPickAudioFormat(t *testing.T) {
	audio := func(url string, bitrate int) format {
		return format{URL: url, MimeType: "audio/mp4; codecs=\"mp4a.40.2\"", Bitrate: bitrate}
	}
	video := format{URL: "https://cdn/video", MimeType: "video/mp4", Bitrate: 900000}
	ciphered := format{MimeType: "audio/webm", Bitrate: 150000, SignatureCipher: "s=..."}

	t.Run("highest bitrate audio wins, video ignored", func(t *testing.T) {
		got, err := pickAudioFormat([]format{video, audio("https://cdn/a1", 64000), audio("https://cdn/a2", 128000)})
		if err != nil {
			t.Fatalf("pickAudioFormat: %v", err)
		}
		if got.URL != "https://cdn/a2" {
			t.Fatalf("picked %q", got.URL)
		}
	})

	t.Run("cipher-only errors for fallback", func(t *testing.T) {
		if _, err := pickAudioFormat([]format{video, ciphered}); !errors.Is(err, ErrCipherOnly) {
			t.Fatalf("err = %v, want ErrCipherOnly", err)
		}
	})

	t.Run("no audio at all", func(t *testing.T) {
		if _, err := pickAudioFormat([]format{video}); !errors.Is(err, ErrNoAudio) {
			t.Fatalf("err = %v, want ErrNoAudio", err)
		}
	})

	t.Run("direct url preferred even if ciphered has higher bitrate", func(t *testing.T) {
		got, err := pickAudioFormat([]format{ciphered, audio("https://cdn/a1", 64000)})
		if err != nil {
			t.Fatalf("pickAudioFormat: %v", err)
		}
		if got.URL != "https://cdn/a1" {
			t.Fatalf("picked %q", got.URL)
		}
	})
}

func TestExtractVideoID(t *testing.T) {
	cases := []struct {
		in   string
		want string
		err  bool
	}{
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ", "dQw4w9WgXcQ", false},
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=42s", "dQw4w9WgXcQ", false},
		{"https://youtu.be/dQw4w9WgXcQ?si=xyz", "dQw4w9WgXcQ", false},
		{"https://www.youtube.com/shorts/dQw4w9WgXcQ", "dQw4w9WgXcQ", false},
		{"https://www.youtube.com/live/dQw4w9WgXcQ", "dQw4w9WgXcQ", false},
		{"https://music.youtube.com/watch?v=dQw4w9WgXcQ&list=RD", "dQw4w9WgXcQ", false},
		{"https://www.youtube.com/embed/dQw4w9WgXcQ", "dQw4w9WgXcQ", false},
		{"https://example.com/notyoutube", "", true},
		{"https://www.youtube.com/watch?v=short", "", true},
	}
	for _, tc := range cases {
		got, err := extractVideoID(tc.in)
		if tc.err {
			if err == nil {
				t.Fatalf("%s: expected error, got %q", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("%s: got %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBitrateCapPicksTheBestThatFits(t *testing.T) {
	// Roughly what YouTube offers for one track: itags 249, 250, 251.
	offered := []format{
		{URL: "u249", MimeType: `audio/webm; codecs="opus"`, Bitrate: 49000},
		{URL: "u250", MimeType: `audio/webm; codecs="opus"`, Bitrate: 66000},
		{URL: "u251", MimeType: `audio/webm; codecs="opus"`, Bitrate: 137000},
	}

	SetMaxBitrate(0)
	defer SetMaxBitrate(0)
	if f, ok := pickOpusFormat(offered); !ok || f.URL != "u251" {
		t.Fatalf("no cap should take the best: %+v", f)
	}

	SetMaxBitrate(70000)
	if f, ok := pickOpusFormat(offered); !ok || f.URL != "u250" {
		t.Fatalf("cap 70k should take 66k: %+v", f)
	}

	SetMaxBitrate(66000)
	if f, ok := pickOpusFormat(offered); !ok || f.URL != "u250" {
		t.Fatalf("the cap is inclusive: %+v", f)
	}

	// A cap under everything on offer must not mean silence.
	SetMaxBitrate(1000)
	if f, ok := pickOpusFormat(offered); !ok || f.URL != "u251" {
		t.Fatalf("an impossible cap should be ignored, got %+v", f)
	}
}

func TestBitrateCapAppliesToTheFfmpegFallbackToo(t *testing.T) {
	offered := []format{
		{URL: "m4a", MimeType: "audio/mp4", Bitrate: 131000},
		{URL: "low", MimeType: "audio/mp4", Bitrate: 50000},
	}
	SetMaxBitrate(60000)
	defer SetMaxBitrate(0)

	f, err := pickAudioFormat(offered)
	if err != nil || f.URL != "low" {
		t.Fatalf("got %+v, err %v", f, err)
	}
}
