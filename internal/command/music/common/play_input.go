package common

import (
	"errors"
	"strconv"
	"strings"
	"unicode"
)

// maxPlayBatchItems limits history ids or URLs enqueued in one /play to avoid huge interactions.
const maxPlayBatchItems = 15

// ErrPlayInputTooManyItems is returned when the parsed id or URL count exceeds maxPlayBatchItems.
var ErrPlayInputTooManyItems = errors.New("too many items in one command")

type PlayInputKind int

const (
	PlayInputKindHistoryIDs PlayInputKind = iota
	PlayInputKindURLs
	PlayInputKindQuery
)

// ParsedPlayInput is the result of ParsePlayInput (no Discord or resolver dependencies).
type ParsedPlayInput struct {
	Kind       PlayInputKind
	HistoryIDs []uint64
	URLs       []string
	// Query is the full trimmed string for a single resolver call (search/title or a lone URL token).
	Query string
}

// ParsePlayInput classifies play text as history ids, multiple URLs, or one resolver query.
// source/parser apply only to the resolver (query/URL) path; callers may ignore them for history ids.
func ParsePlayInput(s string) (ParsedPlayInput, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return ParsedPlayInput{}, errors.New("empty input")
	}

	tokens := splitPlayTokens(trimmed)
	if len(tokens) == 0 {
		return ParsedPlayInput{}, errors.New("empty input")
	}

	if tokensAllNumeric(tokens) {
		ids := make([]uint64, 0, len(tokens))
		for _, t := range tokens {
			id, err := strconv.ParseUint(t, 10, 64)
			if err != nil {
				return ParsedPlayInput{}, err
			}
			ids = append(ids, id)
		}
		if len(ids) > maxPlayBatchItems {
			return ParsedPlayInput{}, ErrPlayInputTooManyItems
		}
		return ParsedPlayInput{Kind: PlayInputKindHistoryIDs, HistoryIDs: ids}, nil
	}

	urls := collectHTTPTokens(tokens)
	if len(urls) >= 2 {
		if len(urls) > maxPlayBatchItems {
			return ParsedPlayInput{}, ErrPlayInputTooManyItems
		}
		return ParsedPlayInput{Kind: PlayInputKindURLs, URLs: urls}, nil
	}

	return ParsedPlayInput{Kind: PlayInputKindQuery, Query: trimmed}, nil
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

