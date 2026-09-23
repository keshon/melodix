package search

import (
	"strings"
	"testing"

	"github.com/keshon/melodix/pkg/music/sources"
)

// The button id is a wire format: ids handed out today come back from choosers
// still sitting in channels tomorrow. These tests pin the shape.

func TestButtonIDRoundTrip(t *testing.T) {
	t.Parallel()
	c := &Search{}

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

	// YouTube rebuilds offline; reaching the network here would be a bug.
	url, err := c.trackURL(source, payload)
	if err != nil || url != "https://www.youtube.com/watch?v=K0HSD_i2DvA" {
		t.Fatalf("url = %q, err = %v", url, err)
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

func TestButtonIDFitsSoundCloudTrackID(t *testing.T) {
	t.Parallel()
	// Track ids are ~10 digits. This is the whole reason the payload is an id
	// and not a permalink, so it is worth pinning.
	id, ok := buttonID(sourceSoundCloud, "1068221248")
	if !ok {
		t.Fatal("a SoundCloud track id must fit")
	}
	if len(id) > customIDLimit {
		t.Fatalf("id is %d chars: %q", len(id), id)
	}
	source, payload, ok := parseButtonID(id)
	if !ok || source != sourceSoundCloud || payload != "1068221248" {
		t.Fatalf("parse = %q, %q, %v", source, payload, ok)
	}
}

func TestButtonIDRejectsOverlongPayload(t *testing.T) {
	t.Parallel()
	// A permalink is what a payload must never be: 8 in 100 real SoundCloud
	// permalinks exceed the budget, Discord would truncate, and the click would
	// come back unroutable. Refuse at build time instead.
	long := "https://soundcloud.com/" + strings.Repeat("x", customIDLimit)
	if _, ok := buttonID(sourceSoundCloud, long); ok {
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

func TestUnknownSourceFailsClosed(t *testing.T) {
	t.Parallel()
	// Forward compatibility: an id minted by a newer build must be rejected, not
	// quietly resolved as YouTube.
	if knownSource("bandcamp") {
		t.Fatal("unknown source reported as known")
	}
	c := &Search{}
	if _, err := c.trackURL("bandcamp", "123"); err == nil {
		t.Fatal("an unknown source must not resolve")
	}
}

func TestPickMapsSourceOption(t *testing.T) {
	t.Parallel()
	c := &Search{}
	cases := []struct {
		option  string
		wantTag string
	}{
		{"", sourceYouTube}, // default
		{sources.YouTube, sourceYouTube},
		{sources.SoundCloud, sourceSoundCloud},
	}
	for _, tc := range cases {
		got, tag, err := c.pick(tc.option)
		if err != nil || got == nil || tag != tc.wantTag {
			t.Errorf("pick(%q) = %v, %q, %v", tc.option, got, tag, err)
		}
	}

	// Radio has nothing to rank, so it is not offered and must be refused.
	if _, _, err := c.pick(sources.Radio); err == nil {
		t.Error("radio must not be searchable")
	}
}

// The button tags are persisted in live choosers, so they are frozen strings.
func TestSourceTagsAreStable(t *testing.T) {
	t.Parallel()
	if sourceYouTube != "yt" || sourceSoundCloud != "sc" {
		t.Fatal("source tags are a wire format and must not be renamed")
	}
}
