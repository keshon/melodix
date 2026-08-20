package youtube

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// browsePage builds a /browse response body with the given entries and an
// optional continuation token.
func browsePage(title string, ids []string, token string) string {
	items := make([]string, 0, len(ids))
	for _, id := range ids {
		items = append(items, fmt.Sprintf(
			`{"playlistVideoRenderer":{"videoId":%q,"title":{"runs":[{"text":"Song %s"}]}}}`, id, id))
	}
	cont := ""
	if token != "" {
		cont = fmt.Sprintf(`,"continuations":[{"nextContinuationData":{"continuation":%q}}]`, token)
	}
	return fmt.Sprintf(`{
		"header":{"playlistHeaderRenderer":{"title":{"runs":[{"text":%q}]}}},
		"contents":{"singleColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"content":{
			"sectionListRenderer":{"contents":[{"playlistVideoListRenderer":{"contents":[%s]%s}}]}}}}]}}
	}`, title, strings.Join(items, ","), cont)
}

func continuationPage(ids []string, token string) string {
	items := make([]string, 0, len(ids))
	for _, id := range ids {
		items = append(items, fmt.Sprintf(
			`{"playlistVideoRenderer":{"videoId":%q,"title":{"runs":[{"text":"Song %s"}]}}}`, id, id))
	}
	cont := ""
	if token != "" {
		cont = fmt.Sprintf(`,"continuations":[{"nextContinuationData":{"continuation":%q}}]`, token)
	}
	return fmt.Sprintf(`{"continuationContents":{"playlistVideoListContinuation":{"contents":[%s]%s}}}`,
		strings.Join(items, ","), cont)
}

func newFetcher(t *testing.T, h http.HandlerFunc) *PlaylistFetcher {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &PlaylistFetcher{BaseURL: srv.URL, Client: srv.Client()}
}

func TestFetchPlaylistFollowsContinuation(t *testing.T) {
	t.Parallel()
	var bodies []map[string]any
	f := newFetcher(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var b map[string]any
		_ = json.Unmarshal(raw, &b)
		bodies = append(bodies, b)
		if _, ok := b["continuation"]; ok {
			fmt.Fprint(w, continuationPage([]string{"ccccccccccc"}, ""))
			return
		}
		fmt.Fprint(w, browsePage("My List", []string{"aaaaaaaaaaa", "bbbbbbbbbbb"}, "TOKEN1"))
	})

	got, err := f.Fetch("PL123456789012", "")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got.Title != "My List" {
		t.Fatalf("title = %q", got.Title)
	}
	if len(got.Entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(got.Entries))
	}
	if got.Entries[0].VideoID != "aaaaaaaaaaa" || got.Entries[0].Title != "Song aaaaaaaaaaa" {
		t.Fatalf("first entry = %+v", got.Entries[0])
	}
	if len(bodies) != 2 {
		t.Fatalf("requests = %d, want 2", len(bodies))
	}
	if bodies[0]["browseId"] != "VLPL123456789012" {
		t.Fatalf("browseId = %v", bodies[0]["browseId"])
	}
	// The continuation request replaces browseId with the token.
	if bodies[1]["continuation"] != "TOKEN1" {
		t.Fatalf("continuation = %v", bodies[1]["continuation"])
	}
	if _, ok := bodies[1]["browseId"]; ok {
		t.Fatal("continuation request must not carry browseId")
	}
}

func TestFetchPlaylistStopsAtCap(t *testing.T) {
	t.Parallel()
	// A server that never stops offering a continuation.
	f := newFetcher(t, func(w http.ResponseWriter, r *http.Request) {
		ids := make([]string, playlistPageSize)
		for i := range ids {
			ids[i] = fmt.Sprintf("id%08d", i)
		}
		fmt.Fprint(w, continuationPage(ids, "MORE"))
	})
	got, err := f.Fetch("PL123456789012", "")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(got.Entries) != maxPlaylistItems {
		t.Fatalf("entries = %d, want cap %d", len(got.Entries), maxPlaylistItems)
	}
}

func TestFetchPlaylistErrorAlert(t *testing.T) {
	t.Parallel()
	f := newFetcher(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"alerts":[{"alertRenderer":{"type":"ERROR","text":{"runs":[{"text":"This playlist type is unviewable."}]}}}]}`)
	})
	_, err := f.Fetch("PL123456789012", "")
	if !errors.Is(err, ErrPlaylistUnavailable) {
		t.Fatalf("err = %v, want ErrPlaylistUnavailable", err)
	}
	// The reason YouTube gave is worth surfacing verbatim.
	if !strings.Contains(err.Error(), "unviewable") {
		t.Fatalf("error should quote the reason: %v", err)
	}
}

func TestFetchPlaylistEmpty(t *testing.T) {
	t.Parallel()
	f := newFetcher(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, browsePage("Empty", nil, ""))
	})
	if _, err := f.Fetch("PL123456789012", ""); !errors.Is(err, ErrPlaylistEmpty) {
		t.Fatalf("err = %v, want ErrPlaylistEmpty", err)
	}
}

func TestFetchPlaylistRejectedIDIsUnavailable(t *testing.T) {
	t.Parallel()
	// A bad, deleted or private list id comes back as 400/404 rather than as an
	// alert, and must still reach the caller as ErrPlaylistUnavailable.
	for _, code := range []int{http.StatusBadRequest, http.StatusNotFound} {
		f := newFetcher(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			fmt.Fprint(w, `{"error":{"code":400,"message":"Request contains an invalid argument."}}`)
		})
		_, err := f.Fetch("PL123456789012", "")
		if !errors.Is(err, ErrPlaylistUnavailable) {
			t.Fatalf("status %d: err = %v, want ErrPlaylistUnavailable", code, err)
		}
		// The raw API body would be noise in a chat reply.
		if strings.Contains(err.Error(), "invalid argument") {
			t.Fatalf("status %d: error should not carry the API body: %v", code, err)
		}
	}
}

func TestFetchMixServerErrorKeepsDetail(t *testing.T) {
	t.Parallel()
	// 5xx is not a "playlist is unavailable" answer, so the body survives for
	// diagnosis instead of being flattened into the sentinel.
	f := newFetcher(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":{"message":"Precondition check failed."}}`)
	})
	_, err := f.Fetch("RDdQw4w9WgXcQ", "")
	if errors.Is(err, ErrPlaylistUnavailable) {
		t.Fatalf("5xx should not map to ErrPlaylistUnavailable: %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "Precondition check failed") {
		t.Fatalf("error should quote the API body: %v", err)
	}
}

func TestFetchMixUsesNextWithSeed(t *testing.T) {
	t.Parallel()
	var path string
	var body map[string]any
	f := newFetcher(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		fmt.Fprint(w, `{"contents":{"twoColumnWatchNextResults":{"playlist":{"playlist":{
			"title":"Mix - Something",
			"contents":[
				{"playlistPanelVideoRenderer":{"videoId":"dQw4w9WgXcQ","title":{"simpleText":"Seed"}}},
				{"playlistPanelVideoRenderer":{"videoId":"btPJPFnesV4","title":{"simpleText":"Second"}}}
			]}}}}}`)
	})

	got, err := f.Fetch("RDdQw4w9WgXcQ", "dQw4w9WgXcQ")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if path != "/youtubei/v1/next" {
		t.Fatalf("mix must use /next, got %q", path)
	}
	if body["playlistId"] != "RDdQw4w9WgXcQ" || body["videoId"] != "dQw4w9WgXcQ" {
		t.Fatalf("request body = %v", body)
	}
	if got.Title != "Mix - Something" || len(got.Entries) != 2 {
		t.Fatalf("got %+v", got)
	}
	// simpleText is the other text encoding and must decode too.
	if got.Entries[0].Title != "Seed" {
		t.Fatalf("entry title = %q", got.Entries[0].Title)
	}
}

func TestFetchMixOmitsSeedWhenUnknown(t *testing.T) {
	t.Parallel()
	var body map[string]any
	f := newFetcher(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		fmt.Fprint(w, `{"contents":{"twoColumnWatchNextResults":{"playlist":{"playlist":{
			"title":"Mix","contents":[{"playlistPanelVideoRenderer":{"videoId":"aaaaaaaaaaa","title":{"simpleText":"A"}}}]}}}}}`)
	})
	if _, err := f.Fetch("RDCLAK5uy_abc", ""); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if _, ok := body["videoId"]; ok {
		t.Fatal("videoId must be omitted when there is no seed")
	}
}

func TestFetchMixEmptyPanel(t *testing.T) {
	t.Parallel()
	// VISIONOS answers /next with no playlist panel at all; treat it as empty
	// rather than as a decode failure.
	f := newFetcher(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"contents":{"singleColumnWatchNextResults":{"results":{},"autoplay":{}}}}`)
	})
	if _, err := f.Fetch("RDdQw4w9WgXcQ", ""); !errors.Is(err, ErrPlaylistEmpty) {
		t.Fatalf("err = %v, want ErrPlaylistEmpty", err)
	}
}

func TestShouldExpandList(t *testing.T) {
	t.Parallel()
	cases := []struct {
		url  string
		want bool
		why  string
	}{
		{"https://www.youtube.com/playlist?list=PL123456789012", true, "a list page is only a list"},
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=PL123456789012", false, "names one video inside a playlist"},
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=RDdQw4w9WgXcQ", true, "a mix has no other URL shape"},
		{"https://music.youtube.com/watch?v=dQw4w9WgXcQ&list=RDAMVMdQw4w9WgXcQ", true, "song radio is a mix"},
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ", false, "no list at all"},
		{"https://youtu.be/dQw4w9WgXcQ", false, "short link, no list"},
	}
	for _, c := range cases {
		if got := shouldExpandList(c.url, ExtractListID(c.url)); got != c.want {
			t.Errorf("shouldExpandList(%q) = %v, want %v (%s)", c.url, got, c.want, c.why)
		}
	}
}

func TestExtractIDs(t *testing.T) {
	t.Parallel()
	u := "https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=PL123456789012&index=3"
	if got := ExtractVideoID(u); got != "dQw4w9WgXcQ" {
		t.Fatalf("video id = %q", got)
	}
	if got := ExtractListID(u); got != "PL123456789012" {
		t.Fatalf("list id = %q", got)
	}
	if got := ExtractVideoID("https://youtu.be/dQw4w9WgXcQ"); got != "dQw4w9WgXcQ" {
		t.Fatalf("short link video id = %q", got)
	}
	if got := ExtractListID("https://www.youtube.com/watch?v=dQw4w9WgXcQ"); got != "" {
		t.Fatalf("list id = %q, want empty", got)
	}
}

func TestIsMixID(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"RDdQw4w9WgXcQ", "RDMMdQw4w9WgXcQ", "RDAMVMdQw4w9WgXcQ", "RDCLAK5uy_abc"} {
		if !IsMixID(id) {
			t.Errorf("IsMixID(%q) = false", id)
		}
	}
	for _, id := range []string{"PL123456789012", "UUSJ4gkVC6NrvII8umztf0Ow", "LL"} {
		if IsMixID(id) {
			t.Errorf("IsMixID(%q) = true", id)
		}
	}
}
