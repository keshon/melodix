package ytnative

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// A passthrough body streams for the length of the whole track, which on a
// lossy link is plenty of time for the connection to be cut. Reopening the
// track from the parser costs a fresh player call and — because a seek is
// served by re-fetching from byte zero and discarding — re-downloading
// everything already heard. Two minutes into a 137 kbps track that is well over
// two megabytes before a single packet plays again, which is heard as a long
// silence and gets more expensive the further the track has run.
//
// resumingBody removes the reason to reopen at all. The demuxer above does not
// care where bytes come from as long as they arrive in order, so a dropped
// connection is repaired underneath it with a ranged request for the very next
// byte. Nothing above notices, no EBML state is lost, and no cluster boundaries
// have to be found. Recovery upstairs stays as the outer net for the cases this
// cannot fix — an expired URL, a video pulled mid-play.
const (
	// maxResumeAttempts bounds consecutive repairs. The count resets as soon as
	// bytes flow again, so a long track on a flaky link can be repaired many
	// times; what it stops is spinning on a source that is simply gone.
	maxResumeAttempts = 5
	// resumeBackoff is the pause before a repair. A link that just dropped a
	// connection rarely accepts a new one in the same millisecond.
	resumeBackoff = 500 * time.Millisecond
)

// errRangeUnsupported means the server answered a ranged request with the whole
// file, which would replay audio already heard.
var errRangeUnsupported = errors.New("ytnative: server ignored the resume range")

type resumingBody struct {
	client *http.Client
	url    string
	ua     string

	// mu guards body and closed, which the reading goroutine (swapping in a
	// repaired connection) and the closing goroutine both touch. It is never
	// held across a read or an HTTP round trip.
	mu     sync.Mutex
	body   io.ReadCloser
	closed bool

	// offset and attempts belong to the reading goroutine alone.
	offset   int64 // bytes handed upward so far, i.e. where a repair resumes
	attempts int
}

func (b *resumingBody) current() (io.ReadCloser, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.body, b.closed
}

// newResumingBody adopts an already-open response body. The caller keeps
// ownership only until this point; Close releases whichever body is live.
func newResumingBody(client *http.Client, url, userAgent string, body io.ReadCloser) *resumingBody {
	return &resumingBody{client: client, url: url, ua: userAgent, body: body}
}

func (b *resumingBody) Read(p []byte) (int, error) {
	for {
		body, closed := b.current()
		n, err := body.Read(p)
		if n > 0 {
			b.offset += int64(n)
			b.attempts = 0 // progress: this connection is healthy again
			return n, nil  // any error comes back on the next call
		}
		if err == nil {
			return 0, nil
		}
		// A clean end is a clean end — the track is simply over. Only a broken
		// connection is worth repairing.
		if errors.Is(err, io.EOF) || closed {
			return 0, err
		}
		if b.attempts >= maxResumeAttempts {
			return 0, fmt.Errorf("ytnative: stream lost after %d resume attempts at byte %d: %w",
				b.attempts, b.offset, err)
		}
		b.attempts++
		l := logger()
		if rerr := b.resume(body); rerr != nil {
			// Report the original failure: it says why the stream broke, while
			// the repair error only says the repair did not take.
			l.Warn().Str("url_host", hostOf(b.url)).Int64("offset", b.offset).
				Int("attempt", b.attempts).Err(rerr).Msg("ytnative_resume_failed")
			return 0, err
		}
		l.Info().Int64("offset", b.offset).Int("attempt", b.attempts).
			Msg("ytnative_stream_resumed")
	}
}

// resume re-requests the same URL from the first byte not yet delivered. old is
// the connection that just failed, closed here rather than under the lock.
func (b *resumingBody) resume(old io.ReadCloser) error {
	_ = old.Close()
	time.Sleep(resumeBackoff)

	req, err := http.NewRequest(http.MethodGet, b.url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", b.ua)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-", b.offset))

	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	// 206 is the only answer that continues the stream. A 200 would restart it
	// from the beginning and replay audio already played, which is worse than
	// stopping.
	if resp.StatusCode != http.StatusPartialContent {
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return errRangeUnsupported
		}
		return fmt.Errorf("ytnative: resume: cdn %s", resp.Status)
	}
	b.mu.Lock()
	stopped := b.closed
	if !stopped {
		b.body = resp.Body
	}
	b.mu.Unlock()
	if stopped {
		// Closed while the repair was in flight: drop it rather than leaking a
		// live connection nobody will read.
		_ = resp.Body.Close()
		return errors.New("ytnative: stream closed during resume")
	}
	return nil
}

// Close releases whichever connection is live and stops any further repair.
func (b *resumingBody) Close() error {
	b.mu.Lock()
	b.closed = true
	body := b.body
	b.mu.Unlock()
	return body.Close()
}

// hostOf keeps a full signed CDN URL out of the logs.
func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Host
}
