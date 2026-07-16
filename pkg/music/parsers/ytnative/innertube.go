package ytnative

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// Thin InnerTube client using the ANDROID_VR client context (the YouTube app on
// Meta Quest), which returns direct (cipher-free) stream URLs anonymously. The
// plain ANDROID client stopped working in 2026 ("Precondition check failed");
// ANDROID_VR is what yt-dlp ships as a default for the same reason. Deliberately
// NO signature/nsig deciphering — that treadmill belongs to kkdai/yt-dlp, which
// stay registered as fallbacks. When this client can't produce a plain URL it
// fails fast and the recovery chain moves on.
const (
	clientName = "ANDROID_VR"
	// clientVersion is THE maintenance knob of this package: when YouTube
	// deprecates it, playback falls back to kkdai/yt-dlp and bumping this
	// constant (current YouTube VR app version, see yt-dlp's innertube client
	// list for a known-good value) is the whole fix.
	clientVersion     = "1.65.10"
	deviceMake        = "Oculus"
	deviceModel       = "Quest 3"
	osName            = "Android"
	osVersion         = "12L"
	androidSDKVersion = 32
	clientUserAgent   = "com.google.android.apps.youtube.vr.oculus/" + clientVersion + " (Linux; U; Android 12L; eureka-user Build/SQ3A.220605.009.A1) gzip"
	playerEndpoint    = "https://www.youtube.com/youtubei/v1/player?prettyPrint=false"
)

var (
	ErrCipherOnly  = errors.New("ytnative: only cipher-protected formats available")
	ErrNotPlayable = errors.New("ytnative: video not playable")
	ErrNoAudio     = errors.New("ytnative: no audio formats in player response")
)

type format struct {
	URL             string `json:"url"`
	MimeType        string `json:"mimeType"`
	Bitrate         int    `json:"bitrate"`
	SignatureCipher string `json:"signatureCipher"`
}

type playerResponse struct {
	PlayabilityStatus struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	} `json:"playabilityStatus"`
	StreamingData struct {
		AdaptiveFormats []format `json:"adaptiveFormats"`
	} `json:"streamingData"`
	VideoDetails struct {
		Title         string `json:"title"`
		LengthSeconds string `json:"lengthSeconds"`
	} `json:"videoDetails"`
}

// fetchPlayer POSTs the ANDROID-client player request. InnerTube accepts keyless
// requests; no poToken or visitorData is sent — if googlevideo URLs start
// returning 403, an "X-Goog-Visitor-Id" header here is the first thing to try.
func fetchPlayer(httpc *http.Client, endpoint, videoID string) (*playerResponse, error) {
	body, err := json.Marshal(map[string]any{
		"context": map[string]any{
			"client": map[string]any{
				"clientName":        clientName,
				"clientVersion":     clientVersion,
				"deviceMake":        deviceMake,
				"deviceModel":       deviceModel,
				"osName":            osName,
				"osVersion":         osVersion,
				"androidSdkVersion": androidSDKVersion,
				"userAgent":         clientUserAgent,
				"hl":                "en",
			},
		},
		"videoId":        videoID,
		"contentCheckOk": true,
		"racyCheckOk":    true,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", clientUserAgent)

	resp, err := httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// Include the API's error body — it names the actual reason
		// (e.g. "Precondition check failed" when a client context is retired).
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return nil, fmt.Errorf("ytnative: player request: %s: %s", resp.Status, strings.Join(strings.Fields(string(snippet)), " "))
	}

	var pr playerResponse
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return nil, fmt.Errorf("ytnative: decode player response: %w", err)
	}
	if pr.PlayabilityStatus.Status != "OK" {
		return nil, fmt.Errorf("%w: %s (%s)", ErrNotPlayable, pr.PlayabilityStatus.Status, pr.PlayabilityStatus.Reason)
	}
	return &pr, nil
}

// pickAudioFormat returns the highest-bitrate audio format with a direct URL.
// Audio formats that exist only as signatureCipher mean this client context is
// being served protected streams — fail fast so the fallback parsers engage.
func pickAudioFormat(formats []format) (format, error) {
	var best format
	cipherOnly := false
	for _, f := range formats {
		if !strings.HasPrefix(f.MimeType, "audio/") {
			continue
		}
		if f.URL == "" {
			if f.SignatureCipher != "" {
				cipherOnly = true
			}
			continue
		}
		if f.Bitrate > best.Bitrate {
			best = f
		}
	}
	if best.URL == "" {
		if cipherOnly {
			return format{}, ErrCipherOnly
		}
		return format{}, ErrNoAudio
	}
	return best, nil
}

var videoIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// extractVideoID handles watch?v=, youtu.be/, shorts/ and live/ URL shapes.
// Kept local (kkdai has an unexported equivalent) so this package stays
// self-contained and the fallback parsers can be dropped someday without refactoring.
func extractVideoID(rawURL string) (string, error) {
	s := rawURL
	for _, marker := range []string{"youtu.be/", "/shorts/", "/live/", "v=", "/embed/"} {
		if i := strings.Index(s, marker); i >= 0 {
			s = s[i+len(marker):]
			break
		}
	}
	for _, sep := range []string{"?", "&", "#", "/"} {
		if i := strings.Index(s, sep); i >= 0 {
			s = s[:i]
		}
	}
	if !videoIDRe.MatchString(s) {
		return "", fmt.Errorf("ytnative: cannot extract video id from %q", rawURL)
	}
	return s, nil
}
