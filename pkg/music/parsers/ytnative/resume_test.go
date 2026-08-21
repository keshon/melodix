package ytnative

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// payload is the "track" these tests stream; every byte is distinct enough that
// a gap or a replay shows up as a mismatch rather than as a plausible stream.
func payload(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i % 251)
	}
	return b
}

// rangeStart reads the first-byte-pos of a "bytes=N-" request header.
func rangeStart(t *testing.T, r *http.Request) int64 {
	t.Helper()
	h := r.Header.Get("Range")
	if h == "" {
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(h, "bytes="), "-"), 10, 64)
	if err != nil {
		t.Fatalf("unparsable Range %q: %v", h, err)
	}
	return n
}

// cuttingServer serves data but hangs up after cutAfter bytes on each of the
// first cuts responses — a connection reset partway through, which is the
// failure this whole file exists for.
func cuttingServer(t *testing.T, data []byte, cutAfter int, cuts int) *httptest.Server {
	t.Helper()
	served := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := rangeStart(t, r)
		if start > 0 {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(data)-1, len(data)))
			w.WriteHeader(http.StatusPartialContent)
		}
		rest := data[start:]
		served++
		if served <= cuts && len(rest) > cutAfter {
			_, _ = w.Write(rest[:cutAfter])
			w.(http.Flusher).Flush()
			// Hang up mid-body: the client sees an unexpected EOF / reset.
			if hj, ok := w.(http.Hijacker); ok {
				conn, _, err := hj.Hijack()
				if err == nil {
					_ = conn.Close()
				}
			}
			return
		}
		_, _ = w.Write(rest)
	}))
}

func openResuming(t *testing.T, srv *httptest.Server) *resumingBody {
	t.Helper()
	resp, err := srv.Client().Get(srv.URL)
	if err != nil {
		t.Fatalf("initial GET: %v", err)
	}
	return newResumingBody(srv.Client(), srv.URL, "test-agent", resp.Body)
}

func TestResumingBodyRepairsACutStream(t *testing.T) {
	data := payload(64 << 10)
	srv := cuttingServer(t, data, 8<<10, 1)
	defer srv.Close()

	body := openResuming(t, srv)
	defer body.Close()

	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	// Byte-for-byte: a resume that overlaps would replay audio, one that skips
	// would drop it, and either shows up here.
	if len(got) != len(data) {
		t.Fatalf("read %d bytes, want %d", len(got), len(data))
	}
	for i := range got {
		if got[i] != data[i] {
			t.Fatalf("byte %d differs — the stream is not contiguous", i)
		}
	}
}

func TestResumingBodySurvivesRepeatedCuts(t *testing.T) {
	data := payload(64 << 10)
	// Cut every response but the last: the attempt counter must reset on
	// progress, or a long track on a bad link would run out of attempts.
	srv := cuttingServer(t, data, 4<<10, maxResumeAttempts+3)
	defer srv.Close()

	body := openResuming(t, srv)
	defer body.Close()

	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("ReadAll after repeated cuts: %v", err)
	}
	if len(got) != len(data) {
		t.Fatalf("read %d bytes, want %d", len(got), len(data))
	}
}

func TestResumingBodyResumesFromTheRightByte(t *testing.T) {
	data := payload(32 << 10)
	var ranges []int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := rangeStart(t, r)
		ranges = append(ranges, start)
		if start > 0 {
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(data[start:])
			return
		}
		_, _ = w.Write(data[:1024])
		w.(http.Flusher).Flush()
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				_ = conn.Close()
			}
		}
	}))
	defer srv.Close()

	body := openResuming(t, srv)
	defer body.Close()

	got, _ := io.ReadAll(body)
	if len(got) != len(data) {
		t.Fatalf("read %d bytes, want %d", len(got), len(data))
	}
	if len(ranges) != 2 || ranges[0] != 0 || ranges[1] != 1024 {
		t.Fatalf("ranges = %v, want the repair to ask for byte 1024 onwards", ranges)
	}
}

func TestResumingBodyRefusesAServerThatIgnoresRange(t *testing.T) {
	data := payload(16 << 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always 200 from the beginning, Range or not. Continuing would replay
		// audio the listener already heard.
		_, _ = w.Write(data[:512])
		w.(http.Flusher).Flush()
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				_ = conn.Close()
			}
		}
	}))
	defer srv.Close()

	body := openResuming(t, srv)
	defer body.Close()

	got, err := io.ReadAll(body)
	if err == nil {
		t.Fatal("expected the read to fail rather than replay from the start")
	}
	if len(got) != 512 {
		t.Fatalf("delivered %d bytes; nothing beyond the cut should be handed up", len(got))
	}
}

func TestResumingBodyPassesCleanEOFThrough(t *testing.T) {
	data := payload(4 << 10)
	reqs := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs++
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	body := openResuming(t, srv)
	defer body.Close()

	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(got) != len(data) {
		t.Fatalf("read %d bytes, want %d", len(got), len(data))
	}
	// A track that simply ended must not be retried.
	if reqs != 1 {
		t.Fatalf("made %d requests, want 1 — a clean end is not a failure", reqs)
	}
}

func TestResumingBodyStopsAfterClose(t *testing.T) {
	data := payload(16 << 10)
	srv := cuttingServer(t, data, 1024, 100) // never completes
	defer srv.Close()

	body := openResuming(t, srv)
	if _, err := body.Read(make([]byte, 512)); err != nil {
		t.Fatalf("first read: %v", err)
	}
	if err := body.Close(); err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("Close: %v", err)
	}
	// After Close the reader must give up rather than keep reconnecting.
	buf := make([]byte, 512)
	for i := 0; i < 4; i++ {
		if _, err := body.Read(buf); err != nil {
			return
		}
	}
	t.Fatal("reads kept succeeding after Close")
}

// TestResumingBodyCloseDuringABlockedReadIsPrompt covers the case that actually
// happens on /stop and /next: the reader is parked waiting for bytes when the
// stream is closed underneath it.
//
// The failure it guards against is not a wrong answer but a slow one. Sampling
// the closed flag before the read rather than after leaves the repair path
// thinking the stream is still wanted, so it sleeps out its backoff and spends a
// request before discovering otherwise — with teardown blocked behind it.
func TestResumingBodyCloseDuringABlockedReadIsPrompt(t *testing.T) {
	var requests int32
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-block // hold the body open, delivering nothing
	}))
	defer srv.Close()
	defer close(block)

	resp, err := srv.Client().Get(srv.URL)
	if err != nil {
		t.Fatalf("initial GET: %v", err)
	}
	body := newResumingBody(srv.Client(), srv.URL, "test-agent", resp.Body)

	readDone := make(chan error, 1)
	go func() {
		_, rerr := body.Read(make([]byte, 512))
		readDone <- rerr
	}()

	// Let the read park, then close underneath it.
	time.Sleep(50 * time.Millisecond)
	start := time.Now()
	_ = body.Close()

	select {
	case <-readDone:
	case <-time.After(resumeBackoff):
		t.Fatalf("read did not return within %v of Close — it went down the repair path", resumeBackoff)
	}

	if elapsed := time.Since(start); elapsed >= resumeBackoff {
		t.Fatalf("Close took %v; a backoff was slept through", elapsed)
	}
	if n := atomic.LoadInt32(&requests); n != 1 {
		t.Fatalf("made %d requests; a closed stream must not be repaired", n)
	}
}
