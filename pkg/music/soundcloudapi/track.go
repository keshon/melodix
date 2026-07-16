package soundcloudapi

import (
	"errors"
	"net/url"
	"strings"
)

var (
	ErrNoResults     = errors.New("soundcloud api: no tracks found")
	ErrNoTranscoding = errors.New("soundcloud api: no playable transcoding")
)

type Transcoding struct {
	URL    string `json:"url"`
	Preset string `json:"preset"` // e.g. "aac_160k", "mp3_standard", "opus_0_0"
	Format struct {
		Protocol string `json:"protocol"` // "hls" | "progressive"
		MimeType string `json:"mime_type"`
	} `json:"format"`
}

type Track struct {
	Title        string `json:"title"`
	DurationMS   int64  `json:"duration"`
	PermalinkURL string `json:"permalink_url"`
	Media        struct {
		Transcodings []Transcoding `json:"transcodings"`
	} `json:"media"`
}

// ResolveTrack turns a soundcloud.com track URL into track metadata + transcodings.
func (c *Client) ResolveTrack(trackURL string) (*Track, error) {
	var t Track
	if err := c.getJSON(c.APIBase+"/resolve?url="+url.QueryEscape(trackURL), &t); err != nil {
		return nil, err
	}
	if len(t.Media.Transcodings) == 0 {
		return nil, ErrNoTranscoding
	}
	return &t, nil
}

// StreamURL exchanges a transcoding for its signed CDN URL (an m3u8 for HLS).
// The URL expires in ~30 minutes — use it immediately, never store it.
func (c *Client) StreamURL(t Transcoding) (string, error) {
	var out struct {
		URL string `json:"url"`
	}
	if err := c.getJSON(t.URL, &out); err != nil {
		return "", err
	}
	if out.URL == "" {
		return "", errors.New("soundcloud api: empty stream url")
	}
	return out.URL, nil
}

// PickTranscoding prefers AAC HLS, then any HLS, then progressive — matching
// SoundCloud's migration to AAC HLS (progressive and HLS MP3/Opus are being
// phased out of their APIs).
func PickTranscoding(ts []Transcoding) (Transcoding, error) {
	var best Transcoding
	bestScore := 0
	for _, t := range ts {
		var score int
		switch {
		case t.Format.Protocol == "hls" && isAAC(t):
			score = 3
		case t.Format.Protocol == "hls":
			score = 2
		case t.Format.Protocol == "progressive":
			score = 1
		}
		if score > bestScore {
			best, bestScore = t, score
		}
	}
	if bestScore == 0 {
		return Transcoding{}, ErrNoTranscoding
	}
	return best, nil
}

func isAAC(t Transcoding) bool {
	return strings.Contains(t.Preset, "aac") || strings.Contains(t.Format.MimeType, "audio/mp4")
}

// SearchFirstTrack returns the top track for a text query via api-v2 search.
func (c *Client) SearchFirstTrack(query string) (*Track, error) {
	var out struct {
		Collection []Track `json:"collection"`
	}
	if err := c.getJSON(c.APIBase+"/search/tracks?q="+url.QueryEscape(query)+"&limit=1", &out); err != nil {
		return nil, err
	}
	if len(out.Collection) == 0 {
		return nil, ErrNoResults
	}
	return &out.Collection[0], nil
}
