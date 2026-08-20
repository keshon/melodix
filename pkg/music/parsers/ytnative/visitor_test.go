package ytnative

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// TestMain keeps the package's tests offline. fetchPlayer now bootstraps a
// visitor id on a cold cache, so without this the whole package would reach out
// to the real youtube.com. Both guards matter: lastTry suppresses the attempt,
// and the dead endpoint makes any attempt that slips through fail locally.
func TestMain(m *testing.M) {
	homeEndpoint = "http://127.0.0.1:1"
	suppressBootstrap()
	os.Exit(m.Run())
}

// suppressBootstrap marks a bootstrap as just-attempted, so visitorID returns
// the cached value (initially "") without making a request.
func suppressBootstrap() {
	visitor.mu.Lock()
	visitor.value, visitor.obtained, visitor.lastTry = "", time.Time{}, time.Now()
	visitor.mu.Unlock()
}

// resetVisitor clears the cache for one test and restores the suppressed state
// afterwards, so a later test in the package never inherits a live bootstrap.
func resetVisitor(t *testing.T) {
	t.Helper()
	visitor.mu.Lock()
	visitor.value, visitor.obtained, visitor.lastTry = "", time.Time{}, time.Time{}
	visitor.mu.Unlock()
	t.Cleanup(suppressBootstrap)
}

// The id is embedded in the page as a JSON string, so it arrives escaped.
func TestFetchVisitorID_UnescapesJSONString(t *testing.T) {
	resetVisitor(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != bootstrapUA {
			t.Errorf("bootstrap UA = %q, want the browser UA", got)
		}
		fmt.Fprint(w, `<script>ytcfg.set({"INNERTUBE_CONTEXT":{"client":{"visitorData":"CgtBQkNEXzEyMzQ1Ng=="}}});</script>`)
	}))
	defer srv.Close()

	old := homeEndpoint
	homeEndpoint = srv.URL
	defer func() { homeEndpoint = old }()

	got, err := fetchVisitorID(srv.Client())
	if err != nil {
		t.Fatalf("fetchVisitorID: %v", err)
	}
	if want := "CgtBQkNEXzEyMzQ1Ng=="; got != want {
		t.Fatalf("visitorData = %q, want %q", got, want)
	}
}

func TestFetchVisitorID_AbsentIsAnError(t *testing.T) {
	resetVisitor(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<html>no config here</html>")
	}))
	defer srv.Close()

	old := homeEndpoint
	homeEndpoint = srv.URL
	defer func() { homeEndpoint = old }()

	if _, err := fetchVisitorID(srv.Client()); err == nil {
		t.Fatal("expected an error when the page carries no visitorData")
	}
}

// A cached id must reach both the client context and the header, and an id the
// server hands back must be adopted -- even from a refused response.
func TestFetchPlayer_SendsAndAdoptsVisitorID(t *testing.T) {
	resetVisitor(t)
	visitor.mu.Lock()
	visitor.value, visitor.obtained = "CACHED_ID", time.Now()
	visitor.mu.Unlock()

	var gotHeader, gotContext string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Goog-Visitor-Id")
		var req struct {
			Context struct {
				Client struct {
					VisitorData string `json:"visitorData"`
				} `json:"client"`
			} `json:"context"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		gotContext = req.Context.Client.VisitorData
		fmt.Fprint(w, `{"responseContext":{"visitorData":"SERVER_ID"},`+
			`"playabilityStatus":{"status":"LOGIN_REQUIRED","reason":"nope"}}`)
	}))
	defer srv.Close()

	// A refused response is still expected to yield its id.
	if _, err := fetchPlayer(srv.Client(), srv.URL, "ZTidn2dBYbY"); err == nil {
		t.Fatal("expected LOGIN_REQUIRED to surface as an error")
	}
	if gotHeader != "CACHED_ID" {
		t.Fatalf("X-Goog-Visitor-Id = %q, want CACHED_ID", gotHeader)
	}
	if gotContext != "CACHED_ID" {
		t.Fatalf("context visitorData = %q, want CACHED_ID", gotContext)
	}

	visitor.mu.Lock()
	adopted := visitor.value
	visitor.mu.Unlock()
	if adopted != "SERVER_ID" {
		t.Fatalf("adopted id = %q, want SERVER_ID", adopted)
	}
}

// No id available: the request must go out unchanged rather than fail.
func TestFetchPlayer_NoVisitorIDOmitsHeader(t *testing.T) {
	suppressBootstrap() // no id known, and no bootstrap attempt
	t.Cleanup(suppressBootstrap)

	var present bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, present = r.Header["X-Goog-Visitor-Id"]
		fmt.Fprint(w, `{"playabilityStatus":{"status":"OK"},"streamingData":{"adaptiveFormats":[]},`+
			`"videoDetails":{"title":"t","lengthSeconds":"10"}}`)
	}))
	defer srv.Close()

	if _, err := fetchPlayer(srv.Client(), srv.URL, "ZTidn2dBYbY"); err != nil {
		t.Fatalf("fetchPlayer: %v", err)
	}
	if present {
		t.Fatal("X-Goog-Visitor-Id must be omitted when no id is known")
	}
}
