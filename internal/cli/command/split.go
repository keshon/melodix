package command

import (
	"strconv"
	"strings"
)

// AllUintStringTokens reports whether every token parses as a base-10 uint64.
func AllUintStringTokens(ss []string) bool {
	for _, s := range ss {
		if _, err := strconv.ParseUint(s, 10, 64); err != nil {
			return false
		}
	}
	return true
}

// SplitQuoted splits the line by spaces but keeps quoted segments as one token.
func SplitQuoted(s string) []string {
	var out []string
	var buf strings.Builder
	inQuote := false
	for _, r := range s {
		switch {
		case r == '"' || r == '\'':
			inQuote = !inQuote
		case (r == ' ' || r == '\t') && !inQuote:
			if buf.Len() > 0 {
				out = append(out, buf.String())
				buf.Reset()
			}
		default:
			buf.WriteRune(r)
		}
	}
	if buf.Len() > 0 {
		out = append(out, buf.String())
	}
	return out
}
