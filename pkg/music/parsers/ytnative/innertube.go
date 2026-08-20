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

	"github.com/keshon/melodix/pkg/music/innertube"
)

// Thin InnerTube player client. The client identity it presents — VISIONOS, the
// YouTube app on Apple Vision Pro — lives in pkg/music/innertube because the
// youtube source needs the same one; see that package for why the choice is
// load-bearing rather than cosmetic.
//
// Deliberately NO signature/nsig deciphering: the URLs this client returns carry
// no n parameter to solve, and if that ever changes the answer is to fall
// through to the yt-dlp fallback, not to grow a JS engine here. When this client
// can't produce a plain URL it fails fast and the recovery chain moves on.

// Local aliases keep this package's call sites unchanged while the values have a
// single definition. clientVersion is quoted in logs and in the live canary.
const (
	clientName      = innertube.ClientName
	clientVersion   = innertube.ClientVersion
	clientUserAgent = innertube.UserAgent

	playerEndpoint = "https://www.youtube.com/youtubei/v1/player?prettyPrint=false"
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

// fetchPlayer POSTs the player request. InnerTube accepts keyless
// requests; no poToken is sent — visionos is one of the clients that does not
// require one. A visitorData session id is sent when one could be obtained (see
// visitor.go), both in the client context and as X-Goog-Visitor-Id.
func fetchPlayer(httpc *http.Client, endpoint, videoID string) (*playerResponse, error) {
	client := innertube.Client()
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
