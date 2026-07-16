package soundcloudapi

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// scServer serves a homepage+bundle (for client_id) plus the given API handlers.
func scServer(t *testing.T, handlers map[string]http.HandlerFunc) (*httptest.Server, *Client) {
	t.Helper()
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<script src="%s/app.js"></script>`, srv.URL)
	})
	mux.HandleFunc("/app.js", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `client_id:"testid"`)
	})
	for pattern, h := range handlers {
		mux.HandleFunc(pattern, h)
	}
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, newTestClient(srv)
}

func TestResolveTrackDecodes(t *testing.T) {
	_, c := scServer(t, map[string]http.HandlerFunc{
		"/resolve": func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Query().Get("url"); got != "https://soundcloud.com/a/b" {
				t.Errorf("resolve url param = %q", got)
			}
			fmt.Fprint(w, `{
				"title": "Test Track",
				"duration": 215000,
				"permalink_url": "https://soundcloud.com/a/b",
				"media": {"transcodings": [
					{"url": "https://api/t1", "preset": "mp3_standard", "format": {"protocol": "progressive", "mime_type": "audio/mpeg"}},
					{"url": "https://api/t2", "preset": "aac_160k", "format": {"protocol": "hls", "mime_type": "audio/mp4; codecs=\"mp4a.40.2\""}}
				]}
			}`)
		},
	})

	track, err := c.ResolveTrack("https://soundcloud.com/a/b")
	if err != nil {
		t.Fatalf("ResolveTrack: %v", err)
	}
	if track.Title != "Test Track" || track.DurationMS != 215000 {
		t.Fatalf("unexpected track: %+v", track)
	}
	if len(track.Media.Transcodings) != 2 {
		t.Fatalf("transcodings = %d, want 2", len(track.Media.Transcodings))
	}
}

func TestResolveTrackNoTranscodings(t *testing.T) {
	_, c := scServer(t, map[string]http.HandlerFunc{
		"/resolve": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"title": "Playlist Maybe", "media": {"transcodings": []}}`)
		},
	})
	if _, err := c.ResolveTrack("https://soundcloud.com/a/sets/b"); !errors.Is(err, ErrNoTranscoding) {
		t.Fatalf("err = %v, want ErrNoTranscoding", err)
	}
}

func TestPickTranscodingPreference(t *testing.T) {
	mk := func(protocol, preset, mime string) Transcoding {
		tr := Transcoding{URL: "u", Preset: preset}
		tr.Format.Protocol = protocol
		tr.Format.MimeType = mime
		return tr
	}
	aacHLS := mk("hls", "aac_160k", "audio/mp4")
	mp3HLS := mk("hls", "mp3_standard", "audio/mpeg")
	prog := mk("progressive", "mp3_standard", "audio/mpeg")

	cases := []struct {
		name string
		in   []Transcoding
		want string // preset of expected pick
		err  bool
	}{
		{"aac hls wins", []Transcoding{prog, mp3HLS, aacHLS}, "aac_160k", false},
		{"hls over progressive", []Transcoding{prog, mp3HLS}, "mp3_standard", false},
		{"progressive fallback", []Transcoding{prog}, "mp3_standard", false},
		{"empty errors", nil, "", true},
	}
	for _, tc := range cases {
		got, err := PickTranscoding(tc.in)
		if tc.err {
			if err == nil {
				t.Fatalf("%s: expected error", tc.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got.Preset != tc.want {
			t.Fatalf("%s: picked %q, want %q", tc.name, got.Preset, tc.want)
		}
		if tc.name == "hls over progressive" && got.Format.Protocol != "hls" {
			t.Fatalf("%s: picked protocol %q", tc.name, got.Format.Protocol)
		}
	}
}

func TestStreamURL(t *testing.T) {
	srv, c := scServer(t, map[string]http.HandlerFunc{
		"/media/stream": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"url": "https://cdn.example.com/playlist.m3u8?sig=x"}`)
		},
	})
	tr := Transcoding{URL: srv.URL + "/media/stream"}
	got, err := c.StreamURL(tr)
	if err != nil {
		t.Fatalf("StreamURL: %v", err)
	}
	if got != "https://cdn.example.com/playlist.m3u8?sig=x" {
		t.Fatalf("url = %q", got)
	}
}

func TestSearchFirstTrack(t *testing.T) {
	_, c := scServer(t, map[string]http.HandlerFunc{
		"/search/tracks": func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Query().Get("q"); got != "some song" {
				t.Errorf("q = %q", got)
			}
			fmt.Fprint(w, `{"collection": [{"title": "Found", "permalink_url": "https://soundcloud.com/f/ound"}]}`)
		},
	})
	track, err := c.SearchFirstTrack("some song")
	if err != nil {
		t.Fatalf("SearchFirstTrack: %v", err)
	}
	if track.PermalinkURL != "https://soundcloud.com/f/ound" {
		t.Fatalf("permalink = %q", track.PermalinkURL)
	}
}

func TestSearchFirstTrackEmpty(t *testing.T) {
	_, c := scServer(t, map[string]http.HandlerFunc{
		"/search/tracks": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"collection": []}`)
		},
	})
	if _, err := c.SearchFirstTrack("nothing"); !errors.Is(err, ErrNoResults) {
		t.Fatalf("err = %v, want ErrNoResults", err)
	}
}
