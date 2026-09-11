package readme

import "testing"

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
