package common

import (
	"errors"
	"testing"
)

func TestParsePlayInput(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		in      string
		want    ParsedPlayInput
		wantErr error
	}{
		{
			name: "single history id",
			in:   "42",
			want: ParsedPlayInput{Kind: PlayInputKindHistoryIDs, HistoryIDs: []uint64{42}},
		},
		{
			name: "multi history ids",
			in:   "7 8 9",
			want: ParsedPlayInput{Kind: PlayInputKindHistoryIDs, HistoryIDs: []uint64{7, 8, 9}},
		},
		{
			name: "multi history ids semicolon",
			in:   "7;8,9",
			want: ParsedPlayInput{Kind: PlayInputKindHistoryIDs, HistoryIDs: []uint64{7, 8, 9}},
		},
		{
			name: "title with words not all numeric",
			in:   "3 doors down",
			want: ParsedPlayInput{Kind: PlayInputKindQuery, Query: "3 doors down"},
		},
		{
			name: "two urls",
			in:   "https://a.com/foo https://b.com/bar",
			want: ParsedPlayInput{Kind: PlayInputKindURLs, URLs: []string{"https://a.com/foo", "https://b.com/bar"}},
		},
		{
			name: "one url and text is query",
			in:   "check this https://a.com",
			want: ParsedPlayInput{Kind: PlayInputKindQuery, Query: "check this https://a.com"},
		},
		{
			name: "single url only",
			in:   "https://youtu.be/abc",
			want: ParsedPlayInput{Kind: PlayInputKindQuery, Query: "https://youtu.be/abc"},
		},
		{
			name:    "empty",
			in:      "   ",
			wantErr: errors.New(""),
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParsePlayInput(tc.in)
			if tc.wantErr != nil {
				if err == nil {
					t.Fatalf("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParsePlayInput: %v", err)
			}
			if got.Kind != tc.want.Kind {
				t.Errorf("Kind: got %v want %v", got.Kind, tc.want.Kind)
			}
			if len(got.HistoryIDs) != len(tc.want.HistoryIDs) {
				t.Fatalf("HistoryIDs len: got %v want %v", got.HistoryIDs, tc.want.HistoryIDs)
			}
			for i := range tc.want.HistoryIDs {
				if got.HistoryIDs[i] != tc.want.HistoryIDs[i] {
					t.Errorf("HistoryIDs[%d]: got %d want %d", i, got.HistoryIDs[i], tc.want.HistoryIDs[i])
				}
			}
			if len(got.URLs) != len(tc.want.URLs) {
				t.Fatalf("URLs: got %v want %v", got.URLs, tc.want.URLs)
			}
			for i := range tc.want.URLs {
				if got.URLs[i] != tc.want.URLs[i] {
					t.Errorf("URLs[%d]: got %q want %q", i, got.URLs[i], tc.want.URLs[i])
				}
			}
			if got.Query != tc.want.Query {
				t.Errorf("Query: got %q want %q", got.Query, tc.want.Query)
			}
		})
	}
}

func TestParsePlayInputTooManyIDs(t *testing.T) {
	t.Parallel()
	var ids string
	for i := 0; i < maxPlayBatchItems+1; i++ {
		if i > 0 {
			ids += " "
		}
		ids += "1"
	}
	_, err := ParsePlayInput(ids)
	if !errors.Is(err, ErrPlayInputTooManyItems) {
		t.Fatalf("want ErrPlayInputTooManyItems, got %v", err)
	}
}

