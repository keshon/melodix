package readme

import (
	"bytes"
	"testing"
)

// The README headings are plain text while Discord keeps its icons, so this is
// the one place the two renderings diverge on purpose.
func TestPlainCategoryDropsTheIcon(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"variation selector", "ℹ️ Information", "Information"},
		{"plain emoji", "🎵 Music", "Music"},
		{"gear", "⚙️ Settings", "Settings"},
		{"already plain", "Music", "Music"},
		{"no separating space", "🎵Music", "Music"},
		{"digits count as text", "24/7", "24/7"},
		// Nothing to keep: an empty heading would be worse than the icon.
		{"icon only", "🎵", "🎵"},
		{"empty", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := plainCategory(tc.in); got != tc.want {
				t.Fatalf("plainCategory(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// Trailing text must survive intact — the icon is a prefix, and stripping runs
// only until the first letter or digit.
func TestPlainCategoryKeepsInnerPunctuation(t *testing.T) {
	if got := plainCategory("🎵 Music & Radio"); got != "Music & Radio" {
		t.Fatalf("got %q, want %q", got, "Music & Radio")
	}
}

// Both frontends' lists come out of section, so the same weights give them the
// same headings in the same order however the commands were registered.
func TestSectionGroupsByWeightThenName(t *testing.T) {
	line := func(s string) func(*bytes.Buffer) {
		return func(b *bytes.Buffer) { b.WriteString("- " + s + "\n") }
	}
	got := section([]entry{
		{name: "stop", category: "🎵 Music", render: line("stop")},
		{name: "help", category: "ℹ️ Information", render: line("help")},
		{name: "play", category: "🎵 Music", render: line("play")},
	}, map[string]int{"ℹ️ Information": 0, "🎵 Music": 39})

	want := "#### Information\n\n- help\n\n#### Music\n\n- play\n- stop"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}
