package ytnative

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// withChunkSize shrinks the range size for a test so a small payload still
// spans several chunks, and restores it afterwards.
func withChunkSize(t *testing.T, n int64) {
	t.Helper()
	prev := chunkSize
	chunkSize = n
	t.Cleanup(func() { chunkSize = prev })
}

// parseRange reads a bounded "bytes=a-b" request header. ok is false for an
// absent or open-ended header, which is what the fallback path sends.
func parseRange(h string) (start, end int64, ok bool) {
	spec, found := strings.CutPrefix(h, "bytes=")
	if !found {
		return 0, 0, false
	}
	a, b, found := strings.Cut(spec, "-")
	if !found || b == "" {
		return 0, 0, false
	}
	start, err := strconv.ParseInt(a, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	end, err = strconv.ParseInt(b, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return start, end, true
}

// rangeLog records the ranges a server was asked for, in order.
type rangeLog struct {
	mu     sync.Mutex
	starts []int64
}

func (r *rangeLog) add(start int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.starts = append(r.starts, start)
}

func (r *rangeLog) snapshot() []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]int64(nil), r.starts...)
}

// rangedServer serves data as a well-behaved ranged CDN would, recording every
// range asked for. fail, when non-nil, is consulted before each range and can
// refuse it — that is how a flaky chunk is simulated.
func rangedServer(t *testing.T, data []byte, fail func(start int64) bool) (*httptest.Server, *rangeLog) {
	t.Helper()
	log := &rangeLog{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start, end, ok := parseRange(r.Header.Get("Range"))
		if !ok {
			// Open-ended or absent: answer as the pre-chunking path expects.
			_, _ = w.Write(data)
			return
		}
		log.add(start)
		if fail != nil && fail(start) {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if end > int64(len(data))-1 {
			end = int64(len(data)) - 1
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data[start : end+1])
	}))
	t.Cleanup(srv.Close)
	return srv, log
}

func TestOpenCDNBodyReadsTheWholeFileInChunks(t *testing.T) {
	withChunkSize(t, 4<<10)
	data := payload(37 << 10) // deliberately not a whole number of chunks
	srv, log := rangedServer(t, data, nil)

	body, err := openCDNBody(srv.URL)
	if err != nil {
		t.Fatalf("openCDNBody: %v", err)
	}
	defer body.Close()

	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(got) != len(data) {
		t.Fatalf("read %d bytes, want %d", len(got), len(data))
	}
	for i := range got {
		if got[i] != data[i] {
			t.Fatalf("byte %d: got %d, want %d", i, got[i], data[i])
		}
	}
	if _, ok := body.(*chunkedBody); !ok {
		t.Fatalf("expected the chunked path, got %T", body)
	}

	// Every chunk boundary asked for exactly once, in order, starting at zero.
	starts := log.snapshot()
	want := (len(data) + int(chunkSize) - 1) / int(chunkSize)
	if len(starts) != want {
		t.Fatalf("made %d range requests (%v), want %d", len(starts), starts, want)
	}
	for i, s := range starts {
		if s != int64(i)*chunkSize {
			t.Fatalf("request %d asked for byte %d, want %d", i, s, int64(i)*chunkSize)
		}
	}
}

func TestOpenCDNBodyKeepsARequestInFlight(t *testing.T) {
	withChunkSize(t, 4<<10)
	data := payload(64 << 10)
	srv, log := rangedServer(t, data, nil)

	body, err := openCDNBody(srv.URL)
	if err != nil {
		t.Fatalf("openCDNBody: %v", err)
	}
	defer body.Close()

	// Drain only the first chunk. The prefetcher should already have gone after
	// the next ones without being asked — that lead is the point of the design.
	buf := make([]byte, chunkSize)
	if _, err := io.ReadFull(body, buf); err != nil {
		t.Fatalf("ReadFull: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(log.snapshot()) > 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("prefetcher never requested a second chunk (asked for %v)", log.snapshot())
}

func TestOpenCDNBodyRetriesALostChunk(t *testing.T) {
	withChunkSize(t, 4<<10)
	data := payload(16 << 10)

	var mu sync.Mutex
	refused := map[int64]int{}
	// Refuse the chunk at 4 KiB once, then serve it: a blip, not a dead source.
	srv, _ := rangedServer(t, data, func(start int64) bool {
		mu.Lock()
		defer mu.Unlock()
		if start == 4<<10 && refused[start] == 0 {
			refused[start]++
			return true
		}
		return false
	})

	body, err := openCDNBody(srv.URL)
	if err != nil {
		t.Fatalf("openCDNBody: %v", err)
	}
	defer body.Close()

	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(got) != len(data) {
		t.Fatalf("read %d bytes, want %d", len(got), len(data))
	}
	for i := range got {
		if got[i] != data[i] {
			t.Fatalf("byte %d differs after the retry: got %d, want %d", i, got[i], data[i])
		}
	}
}

func TestOpenCDNBodyGivesUpOnADeadChunk(t *testing.T) {
	withChunkSize(t, 4<<10)
	data := payload(16 << 10)
	srv, _ := rangedServer(t, data, func(start int64) bool { return start > 0 })

	body, err := openCDNBody(srv.URL)
	if err != nil {
		t.Fatalf("openCDNBody: %v", err)
	}
	defer body.Close()

	// The first chunk still arrives; the failure surfaces once it is drained,
	// which is where RecoveryStream picks the track up.
	got, err := io.ReadAll(body)
	if err == nil {
		t.Fatal("expected the dead chunk to surface as an error")
	}
	if len(got) != int(chunkSize) {
		t.Fatalf("delivered %d bytes before failing, want the first chunk (%d)", len(got), chunkSize)
	}
	if !strings.Contains(err.Error(), "lost after") {
		t.Fatalf("error does not name the exhausted retries: %v", err)
	}
}

func TestOpenCDNBodyStreamsWhenRangesAreIgnored(t *testing.T) {
	withChunkSize(t, 4<<10)
	data := payload(16 << 10)
	// A CDN that answers every request with the whole file, range or no range.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	body, err := openCDNBody(srv.URL)
	if err != nil {
		t.Fatalf("openCDNBody: %v", err)
	}
	defer body.Close()

	if _, ok := body.(*resumingBody); !ok {
		t.Fatalf("expected the streaming fallback, got %T", body)
	}
	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(got) != len(data) {
		t.Fatalf("read %d bytes, want %d", len(got), len(data))
	}
}

func TestOpenCDNBodyStreamsWhenLengthIsUnknown(t *testing.T) {
	withChunkSize(t, 4<<10)
	data := payload(16 << 10)
	// A source that serves ranges but will not say how long it is — the shape a
	// still-growing stream has, and the reason chunking is not used for it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start, end, ok := parseRange(r.Header.Get("Range"))
		if !ok {
			_, _ = w.Write(data)
			return
		}
		if end > int64(len(data))-1 {
			end = int64(len(data)) - 1
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/*", start, end))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data[start : end+1])
	}))
	defer srv.Close()

	body, err := openCDNBody(srv.URL)
	if err != nil {
		t.Fatalf("openCDNBody: %v", err)
	}
	defer body.Close()

	if _, ok := body.(*resumingBody); !ok {
		t.Fatalf("expected the streaming fallback for an unstated length, got %T", body)
	}
	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(got) != len(data) {
		t.Fatalf("read %d bytes, want %d", len(got), len(data))
	}
}

func TestChunkedBodyCloseStopsThePrefetcher(t *testing.T) {
	withChunkSize(t, 4<<10)
	data := payload(1 << 20)

	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })

	log := &rangeLog{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start, end, ok := parseRange(r.Header.Get("Range"))
		if !ok {
			_, _ = w.Write(data)
			return
		}
		log.add(start)
		if start > 0 {
			// Park the prefetcher mid-request: Close has to cut this short rather
			// than wait it out, or a skip would sit behind a chunk nobody wants.
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		if end > int64(len(data))-1 {
			end = int64(len(data)) - 1
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data[start : end+1])
	}))
	defer srv.Close()

	body, err := openCDNBody(srv.URL)
	if err != nil {
		t.Fatalf("openCDNBody: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- body.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Close blocked on a chunk in flight")
	}

	// Nothing new after teardown.
	before := len(log.snapshot())
	time.Sleep(200 * time.Millisecond)
	if after := len(log.snapshot()); after != before {
		t.Fatalf("prefetcher kept fetching after Close: %d -> %d requests", before, after)
	}
	once.Do(func() { close(release) })
}

func TestChunkedBodyCloseIsIdempotent(t *testing.T) {
	withChunkSize(t, 4<<10)
	data := payload(16 << 10)
	srv, _ := rangedServer(t, data, nil)

	body, err := openCDNBody(srv.URL)
	if err != nil {
		t.Fatalf("openCDNBody: %v", err)
	}
	if err := body.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := body.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestTotalFromContentRange(t *testing.T) {
	cases := []struct {
		header string
		want   int64
		known  bool
	}{
		{"bytes 0-1023/3433755", 3433755, true},
		{"bytes 0-0/1", 1, true},
		{"bytes 0-1023/*", 0, false},
		{"", 0, false},
		{"bytes 0-1023", 0, false},
		{"bytes 0-1023/0", 0, false},
		{"bytes 0-1023/nonsense", 0, false},
	}
	for _, c := range cases {
		got, known := totalFromContentRange(c.header)
		if got != c.want || known != c.known {
			t.Errorf("totalFromContentRange(%q) = (%d, %v), want (%d, %v)", c.header, got, known, c.want, c.known)
		}
	}
}

// waitForRanges blocks until the server has been asked for n ranges, so a test
// can act on a prefetcher that has definitely finished rather than racing it.
func waitForRanges(t *testing.T, log *rangeLog, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(log.snapshot()) >= n {
			time.Sleep(50 * time.Millisecond) // let run() return and close chunks
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("only %d of %d ranges requested", len(log.snapshot()), n)
}

// A stop after the fetcher has already pulled the whole file down is the case
// that matters and the easy one to get wrong: the prefetcher exited cleanly, so
// nothing has recorded an error, and only the closed-vs-ended check stands
// between a skip and a half-length blob committed to the track cache. The file
// here is two chunks so it fits entirely in cur plus the queue, which is what
// lets the fetcher finish without the reader consuming anything — the shape a
// short track on a fast link now has every time.
func TestChunkedBodyCloseIsNotAnEndOfTrack(t *testing.T) {
	withChunkSize(t, 4<<10)
	data := payload(6 << 10)
	srv, log := rangedServer(t, data, nil)

	body, err := openCDNBody(srv.URL)
	if err != nil {
		t.Fatalf("openCDNBody: %v", err)
	}
	waitForRanges(t, log, 2) // the opening probe, then the tail

	if err := body.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	buf := make([]byte, 1<<10)
	var total int
	for {
		n, rerr := body.Read(buf)
		total += n
		if rerr != nil {
			err = rerr
			break
		}
	}
	if errors.Is(err, io.EOF) {
		t.Fatalf("a closed stream reported io.EOF after %d bytes, which reads as a finished track", total)
	}
	if !errors.Is(err, errStreamClosed) {
		t.Fatalf("error does not name the close: %v", err)
	}
}

// The same guarantee when the close lands while a chunk is still in flight,
// which takes a different route to the same verdict (see fetchChunk).
func TestChunkedBodyCloseMidFetchIsNotAnEndOfTrack(t *testing.T) {
	withChunkSize(t, 4<<10)
	data := payload(1 << 20)
	srv, _ := rangedServer(t, data, nil)

	body, err := openCDNBody(srv.URL)
	if err != nil {
		t.Fatalf("openCDNBody: %v", err)
	}
	if err := body.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	buf := make([]byte, 32<<10)
	for {
		_, err = body.Read(buf)
		if err != nil {
			break
		}
	}
	if errors.Is(err, io.EOF) {
		t.Fatal("a closed stream reported io.EOF, which reads as a finished track")
	}
	if !errors.Is(err, errStreamClosed) {
		t.Fatalf("error does not name the close: %v", err)
	}
}

func TestChunkedBodyEndOfFileIsEOF(t *testing.T) {
	withChunkSize(t, 4<<10)
	data := payload(9 << 10)
	srv, _ := rangedServer(t, data, nil)

	body, err := openCDNBody(srv.URL)
	if err != nil {
		t.Fatalf("openCDNBody: %v", err)
	}
	defer body.Close()

	buf := make([]byte, 4<<10)
	var total int
	for {
		n, rerr := body.Read(buf)
		total += n
		if rerr != nil {
			err = rerr
			break
		}
	}
	if !errors.Is(err, io.EOF) {
		t.Fatalf("reading to the end gave %v, want io.EOF", err)
	}
	if total != len(data) {
		t.Fatalf("read %d bytes before EOF, want %d", total, len(data))
	}
}

// A 206 carrying no bytes would leave the fetcher asking for the same offset
// forever, so it has to be an error rather than a retry: the track stops and
// RecoveryStream moves on instead of the guild hearing silence with nothing in
// the log.
func TestOpenCDNBodyRefusesAnEmptyChunk(t *testing.T) {
	withChunkSize(t, 4<<10)
	data := payload(16 << 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start, end, ok := parseRange(r.Header.Get("Range"))
		if !ok {
			_, _ = w.Write(data)
			return
		}
		if end > int64(len(data))-1 {
			end = int64(len(data)) - 1
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
		w.WriteHeader(http.StatusPartialContent)
		if start > 0 {
			return // a range answered with nothing at all
		}
		_, _ = w.Write(data[start : end+1])
	}))
	defer srv.Close()

	body, err := openCDNBody(srv.URL)
	if err != nil {
		t.Fatalf("openCDNBody: %v", err)
	}
	defer body.Close()

	done := make(chan error, 1)
	go func() {
		_, rerr := io.ReadAll(body)
		done <- rerr
	}()
	select {
	case err = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("reader hung on an empty chunk instead of failing")
	}
	if err == nil {
		t.Fatal("an empty chunk was accepted")
	}
	if !strings.Contains(err.Error(), "empty chunk") {
		t.Fatalf("error does not name the empty chunk: %v", err)
	}
}

// The final chunk is a partial one, and the request for it has to be clamped to
// the file's last byte rather than asking for a whole chunk past the end. A
// compliant CDN clamps an over-long range for us and hides the mistake; one
// that answers 416 instead would lose the tail of every track.
func TestOpenCDNBodyDoesNotAskPastTheEnd(t *testing.T) {
	withChunkSize(t, 4<<10)
	data := payload(10 << 10) // two full chunks plus a 2 KiB tail
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start, end, ok := parseRange(r.Header.Get("Range"))
		if !ok {
			_, _ = w.Write(data)
			return
		}
		if end > int64(len(data))-1 {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data[start : end+1])
	}))
	defer srv.Close()

	body, err := openCDNBody(srv.URL)
	if err != nil {
		t.Fatalf("openCDNBody: %v", err)
	}
	defer body.Close()

	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(got) != len(data) {
		t.Fatalf("read %d bytes, want the whole file (%d)", len(got), len(data))
	}
	if !bytes.Equal(got, data) {
		t.Fatal("bytes differ from the source")
	}
}
