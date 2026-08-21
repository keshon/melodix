package radio

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

var validContentTypes = []string{
	"audio/", // General catch
	"video/",
	"application/vnd.apple.mpegurl",
	"application/x-mpegurl",
	"application/ogg",
	"application/x-scpls",
	"application/xspf+xml",
	"application/octet-stream", // risky but often used for streams
}

// Validator validates streaming radio links by checking headers and heuristics.
type Validator struct {
	Client *http.Client
}

// NewValidator creates a Validator with production defaults.
func NewValidator() *Validator {
	return &Validator{
		Client: &http.Client{
			Timeout: 5 * time.Second,
			// Redirects are still followed by net/http; this hook only counts
			// them, tightening the default limit from 10 to 5. Station URLs
			// routinely redirect once or twice to a regional edge, never five.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
	}
}

// IsValidURL checks stream validity based on headers, content-type, and file extension heuristics.
func (r *Validator) IsValidURL(rawURL string) (bool, string, error) {
	contentType, finalURL, err := r.fetchContentType(rawURL)
	if err != nil {
		return false, "", fmt.Errorf("failed to fetch content type: %w", err)
	}

	if r.isAllowedType(contentType) || r.isLikelyPlaylist(finalURL) {
		return true, contentType, nil
	}

	// The rejected content-type and the post-redirect URL both go in the error:
	// this is the one place a user finds out why their station link was refused,
	// and "invalid stream" alone leaves nothing to act on.
	return false, contentType, fmt.Errorf("invalid stream content-type: %q, url: %s", contentType, finalURL)
}

func (r *Validator) fetchContentType(rawURL string) (string, string, error) {
	req, err := http.NewRequest(http.MethodHead, rawURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("request creation failed: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := r.Client.Do(req)
	if err != nil || resp.StatusCode >= 400 {
		// HEAD is tried first because it costs no audio, but streaming servers
		// commonly answer it with an error or not at all — so a 4xx/5xx here is
		// not evidence the station is down, and GET has to be asked before the
		// URL can be called invalid. The body is drained and discarded below.
		req.Method = http.MethodGet
		resp, err = r.Client.Do(req)
		if err != nil {
			return "", "", fmt.Errorf("GET fallback failed: %w", err)
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body) // drain the body
	} else {
		defer resp.Body.Close()
	}

	contentType := resp.Header.Get("Content-Type")
	finalURL := resp.Request.URL.String() // actual URL after redirects
	return contentType, finalURL, nil
}

func (r *Validator) isAllowedType(contentType string) bool {
	// Normalize and strip params like "audio/mpeg; charset=utf-8"
	if idx := strings.Index(contentType, ";"); idx != -1 {
		contentType = strings.TrimSpace(contentType[:idx])
	}
	for _, allowed := range validContentTypes {
		if strings.HasPrefix(contentType, allowed) {
			return true
		}
	}
	return false
}

func (r *Validator) isLikelyPlaylist(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	ext := strings.ToLower(path.Ext(u.Path))
	switch ext {
	case ".m3u", ".m3u8", ".pls", ".xspf", ".asx":
		return true
	}
	return false
}
