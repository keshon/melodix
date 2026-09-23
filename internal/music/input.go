package music

import (
	"errors"
	"strconv"
	"strings"
	"unicode"
)

// maxBatchItems limits the history ids or URLs one request may queue, so a
// single command cannot hold a guild's lane for a hundred resolves.
const maxBatchItems = 15

// ErrTooManyItems is returned when the parsed id or URL count exceeds
// maxBatchItems.
var ErrTooManyItems = errors.New("too many items in one request")

// InputKind is what a play request names.
type InputKind int

const (
	InputHistoryIDs InputKind = iota
	InputURLs
	InputQuery
)

// Input is a parsed play request: history ids, several URLs, or one query.
type Input struct {
	Kind       InputKind
	HistoryIDs []uint64
	URLs       []string
	// Query is the full trimmed string for a single resolver call (search/title
	// or a lone URL token).
	Query string
}

// ParseInput classifies play text as history ids, multiple URLs, or one
// resolver query. source/parser apply only to the resolver (query/URL) path;
// callers may ignore them for history ids.
func ParseInput(s string) (Input, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return Input{}, errors.New("empty input")
	}

	tokens := splitPlayTokens(trimmed)
	if len(tokens) == 0 {
		return Input{}, errors.New("empty input")
	}

	if tokensAllNumeric(tokens) {
		ids := make([]uint64, 0, len(tokens))
		for _, t := range tokens {
			id, err := strconv.ParseUint(t, 10, 64)
			if err != nil {
				return Input{}, err
			}
			ids = append(ids, id)
		}
		if len(ids) > maxBatchItems {
			return Input{}, ErrTooManyItems
		}
		return Input{Kind: InputHistoryIDs, HistoryIDs: ids}, nil
	}

	urls := collectHTTPTokens(tokens)
	if len(urls) >= 2 {
		if len(urls) > maxBatchItems {
			return Input{}, ErrTooManyItems
		}
		return Input{Kind: InputURLs, URLs: urls}, nil
	}

	return Input{Kind: InputQuery, Query: trimmed}, nil
}

func splitPlayTokens(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || r == ';' || r == ','
	})
}

func tokensAllNumeric(tokens []string) bool {
	for _, t := range tokens {
		if t == "" {
			return false
		}
		for _, r := range t {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

func isHTTPURL(s string) bool {
	ls := strings.ToLower(s)
	return strings.HasPrefix(ls, "http://") || strings.HasPrefix(ls, "https://")
}

func collectHTTPTokens(tokens []string) []string {
	var out []string
	for _, t := range tokens {
		if isHTTPURL(t) {
			out = append(out, t)
		}
	}
	return out
}
