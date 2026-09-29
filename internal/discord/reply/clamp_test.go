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

func TestChunkEmbedDescriptionShort(t *testing.T) {
	chunks := ChunkEmbedDescription("a\nb", 4000)
	if len(chunks) != 1 || chunks[0] != "a\nb" {
		t.Fatalf("got %q", chunks)
	}
}

func TestChunkEmbedDescriptionEmpty(t *testing.T) {
	if chunks := ChunkEmbedDescription("", 4000); len(chunks) != 0 {
		t.Fatalf("got %q", chunks)
	}
}

func TestChunkEmbedDescriptionSplitsOnLines(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 100; i++ {
		sb.WriteString("`cmd` - description number " + strings.Repeat("x", 40) + "\n")
	}
	chunks := ChunkEmbedDescription(sb.String(), 1000)
	if len(chunks) < 2 {
		t.Fatalf("expected several chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if n := len([]rune(c)); n > 1000 {
			t.Fatalf("chunk %d has %d runes", i, n)
		}
	}
	// Rejoining must not lose entries: every line lands in exactly one chunk.
	joined := strings.Join(chunks, "\n")
	if strings.Count(joined, "`cmd`") != 100 {
		t.Fatalf("lost entries: %d", strings.Count(joined, "`cmd`"))
	}
}

func TestChunkEmbedDescriptionHardSplitsLongLine(t *testing.T) {
	line := strings.Repeat("y", 2500)
	chunks := ChunkEmbedDescription(line, 1000)
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks", len(chunks))
	}
	for i, c := range chunks {
		if n := len([]rune(c)); n > 1000 {
			t.Fatalf("chunk %d has %d runes", i, n)
		}
	}
}

func TestChunkEmbedDescriptionUnicodeCountsRunes(t *testing.T) {
	line := strings.Repeat("🕯️", 900) // multibyte, 1800 runes
	chunks := ChunkEmbedDescription(line+"\n"+line, 2000)
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks", len(chunks))
	}
}
