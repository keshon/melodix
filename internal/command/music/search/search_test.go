package search

import (
	"strings"
	"testing"
)

// The button id is a wire format: ids handed out today come back from choosers
// still sitting in channels tomorrow. These tests pin the shape.

func TestButtonIDRoundTrip(t *testing.T) {
	t.Parallel()
	id, ok := buttonID(sourceYouTube, "K0HSD_i2DvA")
	if !ok {
		t.Fatal("a YouTube id must always fit")
	}
	if id != "search:yt:K0HSD_i2DvA" {
		t.Fatalf("id = %q", id)
	}

	source, payload, ok := parseButtonID(id)
	if !ok || source != sourceYouTube || payload != "K0HSD_i2DvA" {
		t.Fatalf("parse = %q, %q, %v", source, payload, ok)
	}

	url, ok := trackURL(source, payload)
	if !ok || url != "https://www.youtube.com/watch?v=K0HSD_i2DvA" {
		t.Fatalf("url = %q, %v", url, ok)
	}
}

func TestButtonIDPrefixMatchesCommandName(t *testing.T) {
	t.Parallel()
	// internal/discord routes a component to the command whose name prefixes the
	// customID, so these two drifting apart would silently orphan every button.
	c := &Search{}
	if componentPrefix != c.Name() {
		t.Fatalf("componentPrefix = %q, command name = %q", componentPrefix, c.Name())
	}
}

func TestButtonIDRejectsOverlongPayload(t *testing.T) {
	t.Parallel()
	// A URL-shaped payload is what a future source would carry; Discord would
	// truncate it and the click would come back unroutable, so it must be
	// refused at build time instead.
	long := "https://soundcloud.com/" + strings.Repeat("x", customIDLimit)
	if _, ok := buttonID("sc", long); ok {
		t.Fatal("an overlong payload must be refused")
	}
}

func TestParseButtonIDRejectsGarbage(t *testing.T) {
	t.Parallel()
	for _, id := range []string{
		"",
		"search",
		"search:",
		"search:yt",    // no payload separator
		"search:yt:",   // empty payload
		"search::abc",  // empty source
		"other:yt:abc", // another command's button
		"searchyt:abc", // missing separator
	} {
		if _, _, ok := parseButtonID(id); ok {
			t.Errorf("parseButtonID(%q) accepted", id)
		}
	}
}

func TestTrackURLRejectsUnknownSource(t *testing.T) {
	t.Parallel()
	// Forward compatibility: an id minted by a newer build must fail closed, not
	// resolve as YouTube.
	if _, ok := trackURL("sc", "https://soundcloud.com/a/b"); ok {
		t.Fatal("an unknown source must not resolve")
	}
}
