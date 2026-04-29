package sources

import "strings"

// IsURL reports whether s looks like an http(s) URL.
func IsURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}
