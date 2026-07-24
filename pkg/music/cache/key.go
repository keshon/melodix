// Package cache stores played tracks as blobs of 20ms Opus packets and serves
// them back on later plays (a global, content-keyed track cache). Blobs are the
// engine's native currency, so a cached track replays with no extraction and no
// ffmpeg — instant, and immune to a source changing its internals or going down.
package cache

import (
	"net/url"
	"strings"

	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/sources"
)

// Key returns a stable, source-agnostic content key for a track, or ok=false
// when the track is not cacheable (radio and anything without a stable id).
// There is no id field on a track, so the key is derived from the source name
// plus a normalized URL — the same video/track resolves to one key regardless of
// URL form, so the cache is shared across guilds and URL variants.
func Key(track *parsers.Track) (string, bool) {
	if track == nil {
		return "", false
	}
	return KeyFrom(track.SourceInfo.SourceName, track.URL)
}

// KeyFrom builds the content key from a source name and raw URL.
func KeyFrom(sourceName, rawURL string) (string, bool) {
	switch sourceName {
	case sources.YouTube:
		if id := youtubeVideoID(rawURL); id != "" {
			return "youtube:" + id, true
		}
	case sources.SoundCloud:
		if u := normalizeSoundCloudURL(rawURL); u != "" {
			return "soundcloud:" + u, true
		}
	}
	// Radio (live), auto, and unrecognized URLs are uncacheable.
	return "", false
}

// youtubeVideoID extracts the 11-char video id from any common YouTube URL form
// (watch?v=, youtu.be/, /shorts/, /embed/), or "" if none.
func youtubeVideoID(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	switch {
	case host == "youtu.be":
		return strings.Trim(u.Path, "/")
	case host == "youtube.com" || strings.HasSuffix(host, ".youtube.com"):
		if id := u.Query().Get("v"); id != "" {
			return id
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) == 2 && (parts[0] == "shorts" || parts[0] == "embed") {
			return parts[1]
		}
	}
	return ""
}

// normalizeSoundCloudURL reduces a SoundCloud track URL to a stable host+path
// key (dropping query/fragment/trailing slash), or "" if it isn't one.
func normalizeSoundCloudURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host != "soundcloud.com" && !strings.HasSuffix(host, ".soundcloud.com") {
		return ""
	}
	p := strings.TrimRight(u.Path, "/")
	if p == "" {
		return ""
	}
	return host + p
}
