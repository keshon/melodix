package youtube

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func searchPage(hits ...string) string {
	return fmt.Sprintf(`{"contents":{"sectionListRenderer":{"contents":[{"itemSectionRenderer":{"contents":[%s]}}]}}}`,
		joinComma(hits))
}

func joinComma(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ","
		}
		out += p
	}
	return out
}

func videoHit(id, title, author, length string) string {
	return fmt.Sprintf(`{"compactVideoRenderer":{"videoId":%q,"title":{"simpleText":%q},
		"shortBylineText":{"runs":[{"text":%q}]},"lengthText":{"simpleText":%q}}}`, id, title, author, length)
}

func newSearcher(t *testing.T, h http.HandlerFunc) *Searcher {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Searcher{BaseURL: srv.URL, Client: srv.Client()}
}

func TestSearchParsesHits(t *testing.T) {
	t.Parallel()
	var body map[string]any
	s := newSearcher(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		fmt.Fprint(w, searchPage(
			videoHit("K0HSD_i2DvA", "Around The World", "Daft Punk", "4:02"),
			videoHit("dwDns8x3Jb4", "Around the World (Audio)", "Daft Punk", "1:00:02"),
		))
	})

	got, err := s.Search("daft punk", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("results = %d, want 2", len(got))
	}
	if got[0].ID != "K0HSD_i2DvA" || got[0].Title != "Around The World" || got[0].Author != "Daft Punk" {
		t.Fatalf("first = %+v", got[0])
	}
	if got[0].Duration != 242*time.Second {
		t.Fatalf("duration = %v, want 4:02", got[0].Duration)
	}
	if got[1].Duration != 3602*time.Second {
		t.Fatalf("hh:mm:ss duration = %v", got[1].Duration)
	}
	if got[0].URL != "https://www.youtube.com/watch?v=K0HSD_i2DvA" {
		t.Fatalf("url = %q", got[0].URL)
	}
	// Without the filter the results carry channels and playlists too.
	if body["params"] != videoOnlyParams {
		t.Fatalf("params = %v, want the video-only filter", body["params"])
	}
	if body["query"] != "daft punk" {
		t.Fatalf("query = %v", body["query"])
	}
}

func TestSearchRespectsLimit(t *testing.T) {
	t.Parallel()
	s := newSearcher(t, func(w http.ResponseWriter, r *http.Request) {
		hits := make([]string, 20)
		for i := range hits {
			hits[i] = videoHit(fmt.Sprintf("id%08d", i), fmt.Sprintf("Song %d", i), "A", "1:00")
		}
		fmt.Fprint(w, searchPage(hits...))
	})
	got, err := s.Search("q", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("results = %d, want 5", len(got))
	}
}

func TestSearchNoMatches(t *testing.T) {
	t.Parallel()
	s := newSearcher(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, searchPage())
	})
	if _, err := s.Search("nothing", 5); !errors.Is(err, ErrNoVideoMatch) {
		t.Fatalf("err = %v, want ErrNoVideoMatch", err)
	}
}

func TestSearchSkipsNonVideoItems(t *testing.T) {
	t.Parallel()
	// The filter should prevent these, but a stray shelf must not become an
	// empty-id result the user can click.
	s := newSearcher(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, searchPage(
			`{"shelfRenderer":{"title":{"simpleText":"People also watched"}}}`,
			videoHit("K0HSD_i2DvA", "Real", "Someone", "2:00"),
		))
	})
	got, err := s.Search("q", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].ID != "K0HSD_i2DvA" {
		t.Fatalf("got %+v", got)
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	t.Parallel()
	s := newSearcher(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("must not reach the network for an empty query")
	})
	if _, err := s.Search("   ", 5); err == nil {
		t.Fatal("expected an error")
	}
}

func TestParseClockDuration(t *testing.T) {
	t.Parallel()
	cases := map[string]time.Duration{
		"4:02":    242 * time.Second,
		"0:59":    59 * time.Second,
		"1:00:02": 3602 * time.Second,
		"":        0,
		"LIVE":    0,
		"a:b":     0,
		"1:2:3:4": 0,
	}
	for in, want := range cases {
		if got := parseClockDuration(in); got != want {
			t.Errorf("parseClockDuration(%q) = %v, want %v", in, got, want)
		}
	}
}
