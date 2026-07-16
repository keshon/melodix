// Package soundcloudapi is a minimal client for SoundCloud's api-v2 — the API the
// web player itself uses. It handles the rotating client_id (scraped from the web
// player's JS bundles, refreshed automatically on 401/403), track resolving, signed
// stream URLs, and search. Plain net/http + encoding/json, nothing else.
package soundcloudapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sync"
	"time"
)

// Client talks to SoundCloud api-v2 with automatic client_id management.
// Use Default() to share one client_id cache process-wide, or New() for an
// isolated instance (tests).
type Client struct {
	// HTTP, APIBase and WebBase are overridable for tests.
	HTTP    *http.Client
	APIBase string
	WebBase string

	// mu guards clientID; holding it across the scrape also serializes
	// concurrent re-scrapes so rotation triggers one fetch, not many.
	mu       sync.Mutex
	clientID string
}

// New creates a Client with production defaults.
func New() *Client {
	return &Client{
		HTTP:    &http.Client{Timeout: 10 * time.Second},
		APIBase: "https://api-v2.soundcloud.com",
		WebBase: "https://soundcloud.com",
	}
}

var defaultClient = sync.OnceValue(New)

// Default returns the process-wide shared client, so the scnative parser and the
// soundcloud source's searcher reuse one client_id cache.
func Default() *Client { return defaultClient() }

var (
	scriptSrcRe = regexp.MustCompile(`<script[^>]+src="(https?://[^"]+\.js)"`)
	clientIDRe  = regexp.MustCompile(`client_id:"([a-zA-Z0-9]+)"`)
)

// ClientID returns the cached client_id, scraping the web player on first use.
func (c *Client) ClientID() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.clientID != "" {
		return c.clientID, nil
	}
	id, err := c.scrapeClientID()
	if err != nil {
		return "", err
	}
	c.clientID = id
	l := logger()
	l.Debug().Msg("soundcloud_client_id_scraped")
	return id, nil
}

// invalidateClientID drops the cache only if it still holds the id that failed,
// so a concurrent refresh is not thrown away.
func (c *Client) invalidateClientID(failed string) {
	c.mu.Lock()
	if c.clientID == failed {
		c.clientID = ""
	}
	c.mu.Unlock()
}

// scrapeClientID fetches the web player page and greps its JS bundles for the
// client_id. Bundles are tried in reverse order — the id historically lives in
// one of the last chunks. Caller holds c.mu.
func (c *Client) scrapeClientID() (string, error) {
	page, err := c.getBody(c.WebBase)
	if err != nil {
		return "", fmt.Errorf("soundcloud client_id: fetch web player: %w", err)
	}
	matches := scriptSrcRe.FindAllStringSubmatch(page, -1)
	if len(matches) == 0 {
		return "", fmt.Errorf("soundcloud client_id: no script bundles found")
	}
	for i := len(matches) - 1; i >= 0; i-- {
		body, err := c.getBody(matches[i][1])
		if err != nil {
			continue
		}
		if m := clientIDRe.FindStringSubmatch(body); m != nil {
			return m[1], nil
		}
	}
	return "", fmt.Errorf("soundcloud client_id: not found in %d script bundles", len(matches))
}

func (c *Client) getBody(rawURL string) (string, error) {
	resp, err := c.HTTP.Get(rawURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: %s", rawURL, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// getJSON GETs rawURL with client_id appended and decodes the response into v.
// On 401/403 the cached client_id is dropped (it rotates every few days),
// re-scraped, and the request retried exactly once.
func (c *Client) getJSON(rawURL string, v any) error {
	for attempt := 0; ; attempt++ {
		id, err := c.ClientID()
		if err != nil {
			return err
		}
		u, err := url.Parse(rawURL)
		if err != nil {
			return err
		}
		q := u.Query()
		q.Set("client_id", id)
		u.RawQuery = q.Encode()

		resp, err := c.HTTP.Get(u.String())
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			resp.Body.Close()
			if attempt == 0 {
				l := logger()
				l.Warn().Str("status", resp.Status).Msg("soundcloud_client_id_rotated_refreshing")
				c.invalidateClientID(id)
				continue
			}
			return fmt.Errorf("soundcloud api: %s after client_id refresh", resp.Status)
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return fmt.Errorf("soundcloud api: %s", resp.Status)
		}
		err = json.NewDecoder(resp.Body).Decode(v)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("soundcloud api: decode response: %w", err)
		}
		return nil
	}
}
