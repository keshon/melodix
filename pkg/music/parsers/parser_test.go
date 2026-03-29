package parsers

import (
	"testing"

	"github.com/keshon/melodix/pkg/music/sources"
)

func TestTrackParseDisplayLabel(t *testing.T) {
	t.Parallel()
	if got := (TrackParse{Title: "  A  "}).DisplayLabel(); got != "A" {
		t.Fatalf("Title: got %q", got)
	}
	if got := (TrackParse{URL: "https://x", SourceInfo: sources.TrackInfo{Title: "FromSource"}}).DisplayLabel(); got != "FromSource" {
		t.Fatalf("SourceInfo.Title: got %q", got)
	}
	if got := (TrackParse{URL: "https://youtu.be/abc"}).DisplayLabel(); got != "https://youtu.be/abc" {
		t.Fatalf("URL fallback: got %q", got)
	}
	if got := (TrackParse{}).DisplayLabel(); got != "(no title)" {
		t.Fatalf("empty: got %q", got)
	}
}
