package soundcloudapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestClient points a Client at the given test server for both web and API bases.
func newTestClient(srv *httptest.Server) *Client {
	return &Client{
		HTTP:    srv.Client(),
		APIBase: srv.URL,
		WebBase: srv.URL,
	}
}

func TestClientIDScrapedFromLastBundle(t *testing.T) {
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<html><script crossorigin src="%s/a.js"></script><script crossorigin src="%s/b.js"></script></html>`, srv.URL, srv.URL)
	})
	mux.HandleFunc("/a.js", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `var nothing = "here";`)
	})
	bundleHits := 0
	mux.HandleFunc("/b.js", func(w http.ResponseWriter, r *http.Request) {
		bundleHits++
		fmt.Fprint(w, `xyz,client_id:"abc123XYZ",more`)
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(srv)
	id, err := c.ClientID()
	if err != nil {
		t.Fatalf("ClientID: %v", err)
	}
	if id != "abc123XYZ" {
		t.Fatalf("id = %q, want abc123XYZ", id)
	}
	// Cached: second call must not re-scrape.
	if _, err := c.ClientID(); err != nil {
		t.Fatalf("second ClientID: %v", err)
	}
	if bundleHits != 1 {
		t.Fatalf("bundle fetched %d times, want 1 (cache miss)", bundleHits)
	}
}

func TestGetJSONRefreshesRotatedClientID(t *testing.T) {
	var srv *httptest.Server
	currentID := "oldID1"
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<script src="%s/app.js"></script>`, srv.URL)
	})
	mux.HandleFunc("/app.js", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `client_id:"%s"`, currentID)
	})
	mux.HandleFunc("/resolve", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("client_id") != "newID2" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"title":"ok","duration":1000,"media":{"transcodings":[{"url":"u","format":{"protocol":"hls"}}]}}`)
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(srv)
	if _, err := c.ClientID(); err != nil { // prime the cache with the stale id
		t.Fatalf("prime ClientID: %v", err)
	}
	currentID = "newID2" // SoundCloud rotates the id

	track, err := c.ResolveTrack("https://soundcloud.com/x/y")
	if err != nil {
		t.Fatalf("ResolveTrack should survive one rotation: %v", err)
	}
	if track.Title != "ok" {
		t.Fatalf("title = %q, want ok", track.Title)
	}
}

func TestGetJSONFailsAfterSecondUnauthorized(t *testing.T) {
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<script src="%s/app.js"></script>`, srv.URL)
	})
	mux.HandleFunc("/app.js", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `client_id:"whatever"`)
	})
	mux.HandleFunc("/resolve", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(srv)
	_, err := c.ResolveTrack("https://soundcloud.com/x/y")
	if err == nil || !strings.Contains(err.Error(), "after client_id refresh") {
		t.Fatalf("err = %v, want failure after single retry", err)
	}
}
