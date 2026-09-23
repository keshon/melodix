package reply

import (
	"strings"
	"testing"
)

// A raw error can be longer than an embed description may be. Discord rejects
// the whole reply rather than cutting it, so the text is cut here.
func TestClampEmbedTextTruncates(t *testing.T) {
	got := ClampEmbedText(strings.Repeat("x", 4000))
	if len([]rune(got)) > 3501 {
		t.Fatalf("expected truncation around 3500 runes, got %d runes", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected ellipsis suffix, got %q", got)
	}
}

func TestClampEmbedTextKeepsEmpty(t *testing.T) {
	if got := ClampEmbedText(""); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}
