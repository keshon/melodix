package ytnative

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"
)

// visitorData is InnerTube's anonymous session identity. Without it every
// player request looks like a brand-new client with no history, which is one of
// the signals behind LOGIN_REQUIRED ("Sign in to confirm you're not a bot") --
// the failure this package hits first. It is bootstrapped from the YouTube home
// page's ytcfg blob and refreshed from any player response that carries one, so
// a long-running bot keeps one identity instead of minting a fresh one per
// call.
//
// This is a plausibility fix, not a guarantee: LOGIN_REQUIRED is also driven by
// IP reputation, which no header can undo. Failing to obtain an id is therefore
// non-fatal -- requests go out exactly as they did before.

// homeEndpoint is a var so tests can point the bootstrap at an httptest server.
var homeEndpoint = "https://www.youtube.com/"

const (
	// visitorTTL is how long a bootstrapped id is reused. YouTube's own ids are
	// long-lived; this mainly bounds staleness across a multi-day process.
	visitorTTL = 6 * time.Hour
	// visitorRetryWait keeps a failing bootstrap from running on every play.
	visitorRetryWait = 5 * time.Minute
	// bootstrapUA fetches the home page as a browser. An app client's UA gets a
	// page shape that carries no ytcfg blob, so this deliberately does not reuse
	// clientUserAgent.
	bootstrapUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"
)

var visitor struct {
	mu       sync.Mutex
	value    string
	obtained time.Time
	lastTry  time.Time
}

var visitorDataRe = regexp.MustCompile(`"visitorData":"([^"]+)"`)

// visitorID returns the cached id, bootstrapping one when the cache is empty or
// stale. It returns "" when none could be obtained; callers then proceed
// without one. The HTTP call happens under the lock, which serializes a
// concurrent first play across guilds for at most httpc's timeout -- acceptable
// given it runs once per visitorTTL on success and once per visitorRetryWait on
// failure.
func visitorID(httpc *http.Client) string {
	visitor.mu.Lock()
	defer visitor.mu.Unlock()

	if visitor.value != "" && time.Since(visitor.obtained) < visitorTTL {
		return visitor.value
	}
	if !visitor.lastTry.IsZero() && time.Since(visitor.lastTry) < visitorRetryWait {
		return visitor.value // may be "" — recently tried and failed
	}
	visitor.lastTry = time.Now()

	l := logger()
	v, err := fetchVisitorID(httpc)
	if err != nil {
		l.Warn().Err(err).Msg("ytnative_visitor_bootstrap_failed")
		return visitor.value
	}
	visitor.value = v
	visitor.obtained = time.Now()
	l.Info().Msg("ytnative_visitor_bootstrapped")
	return v
}

// rememberVisitorID adopts an id YouTube handed back in a response context,
// which keeps the session identity current without another bootstrap.
func rememberVisitorID(v string) {
	if v == "" {
		return
	}
	visitor.mu.Lock()
	defer visitor.mu.Unlock()
	visitor.value = v
	visitor.obtained = time.Now()
}

// fetchVisitorID scrapes the id out of the home page's ytcfg blob.
func fetchVisitorID(httpc *http.Client) (string, error) {
	req, err := http.NewRequest(http.MethodGet, homeEndpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", bootstrapUA)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := httpc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ytnative: visitor bootstrap: %s", resp.Status)
	}

	// Cap the read: a redesigned page must not stream unbounded HTML into memory.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	m := visitorDataRe.FindSubmatch(body)
	if m == nil {
		return "", errors.New("ytnative: no visitorData on home page")
	}
	// The capture is a JSON string body, so it still carries = escapes.
	v, err := strconv.Unquote(`"` + string(m[1]) + `"`)
	if err != nil {
		return "", fmt.Errorf("ytnative: decode visitorData: %w", err)
	}
	return v, nil
}
