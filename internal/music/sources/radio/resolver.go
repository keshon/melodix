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

// RadioResolver validates streaming radio links by checking headers and heuristics.
type RadioResolver struct {
	Client *http.Client
}

func NewRadioResolver() *RadioResolver {
	return &RadioResolver{
		Client: &http.Client{
			Timeout: 5 * time.Second,
			// Follow redirects manually so we can inspect each step if needed
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
func (r *RadioResolver) IsValidURL(rawURL string) (bool, string, error) {
	contentType, finalURL, err := r.fetchContentType(rawURL)
	if err != nil {
		// Network or request-level failure: big red flag
		return false, "", fmt.Errorf("failed to fetch content type: %w", err)
	}

	if r.isAllowedType(contentType) || r.isLikelyPlaylist(finalURL) {
		return true, contentType, nil
	}

	// Rejected by content-type + extension heuristics: let's not be coy about it
	return false, contentType, fmt.Errorf("invalid stream content-type: %q, url: %s", contentType, finalURL)
}

func (r *RadioResolver) fetchContentType(rawURL string) (string, string, error) {
	req, err := http.NewRequest(http.MethodHead, rawURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("request creation failed: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := r.Client.Do(req)
	if err != nil || resp.StatusCode >= 400 {
		// Try GET as fallback
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

func (r *RadioResolver) isAllowedType(contentType string) bool {
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

func (r *RadioResolver) isLikelyPlaylist(rawURL string) bool {
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
