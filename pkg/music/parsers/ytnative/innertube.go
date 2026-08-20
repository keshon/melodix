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

// Thin InnerTube client using the VISIONOS client context (the YouTube app on
// Apple Vision Pro), which returns direct (cipher-free) stream URLs anonymously
// and needs no PO token. Deliberately NO signature/nsig deciphering — that
// treadmill belongs to kkdai/yt-dlp, which stay registered as fallbacks. When
// this client can't produce a plain URL it fails fast and the chain moves on.
//
// The client choice is not cosmetic, and this is why it is VISIONOS and not
// ANDROID_VR: googlevideo enforces different request rules per issuing client.
// An ANDROID_VR stream URL rejects any open-ended request with 403 — a plain
// GET, or Range: bytes=0- — and serves only bounded ranges up to about 1 MiB.
// Both this package's passthrough (a plain GET) and ffmpeg (Range: bytes=0-)
// ask open-ended, so every ANDROID_VR playback died on its first read. VISIONOS
// URLs serve all three shapes, which is also how yt-dlp streams without a JS
// runtime. Verified against the live CDN, not inferred.
const (
	clientName = "VISIONOS"
	// clientVersion is THE maintenance knob of this package: when YouTube
	// deprecates it, playback falls back to kkdai/yt-dlp and bumping this
	// constant (see yt-dlp's INNERTUBE_CLIENTS for a known-good value) is the
	// whole fix.
	clientVersion   = "1.02"
	deviceMake      = "Apple"
	deviceModel     = "RealityDevice17,1"
	osName          = "visionOS"
	osVersion       = "26.5.23O471"
	clientUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 15_7_3) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15"
	playerEndpoint  = "https://www.youtube.com/youtubei/v1/player?prettyPrint=false"
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
	ResponseContext struct {
		VisitorData string `json:"visitorData"`
	} `json:"responseContext"`
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
// requests; no poToken is sent — android_vr is one of the clients that does not
// require one. A visitorData session id is sent when one could be obtained (see
// visitor.go), both in the client context and as X-Goog-Visitor-Id.
func fetchPlayer(httpc *http.Client, endpoint, videoID string) (*playerResponse, error) {
	client := map[string]any{
		"clientName":    clientName,
		"clientVersion": clientVersion,
		"deviceMake":    deviceMake,
		"deviceModel":   deviceModel,
		"osName":        osName,
		"osVersion":     osVersion,
		"userAgent":     clientUserAgent,
		"hl":            "en",
	}
	vid := visitorID(httpc)
	if vid != "" {
		client["visitorData"] = vid
	}

	body, err := json.Marshal(map[string]any{
		"context":        map[string]any{"client": client},
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
	if vid != "" {
		req.Header.Set("X-Goog-Visitor-Id", vid)
	}

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
	// Adopt before the playability check: a refused response still carries a
	// usable session id, and the next attempt should reuse it.
	rememberVisitorID(pr.ResponseContext.VisitorData)
	if pr.PlayabilityStatus.Status != "OK" {
		return nil, fmt.Errorf("%w: %s (%s)", ErrNotPlayable, pr.PlayabilityStatus.Status, pr.PlayabilityStatus.Reason)
	}
	return &pr, nil
}

// pickOpusFormat returns the highest-bitrate WebM/Opus audio format with a direct
// URL (itag 251/250/249) — the passthrough candidate. ok is false if none exist.
func pickOpusFormat(formats []format) (format, bool) {
	var best format
	for _, f := range formats {
		if f.URL == "" {
			continue
		}
		if !strings.Contains(f.MimeType, "audio/webm") || !strings.Contains(f.MimeType, "opus") {
			continue
		}
		if f.Bitrate > best.Bitrate {
			best = f
		}
	}
	return best, best.URL != ""
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
