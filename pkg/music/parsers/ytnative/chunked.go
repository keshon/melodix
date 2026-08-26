package ytnative

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// googlevideo paces an open-ended response at roughly 1.9x the format's own
// bitrate — measured, not inferred, and at every Opus format it offers (49, 66
// and 137 kbps all came back at the same multiple), so a smaller format buys
// fewer bytes and no extra headroom. Ranges are not paced: the same 3.27 MiB
// track came back in 1.4 seconds, 144x real time. See throughput_live_test.go.
//
// That 1.9x is why the fetch shape is the thing to fix and the buffer is not.
// It left the anti-skip buffer gaining under a second of lead per second
// played, so don't answer a stutter by growing BUFFER_AHEAD_MS or pre-filling
// before playback: a lead accrues only as fast as bytes arrive, 30 seconds of
// it cost 16 seconds of dead air at track start, and a link below 1x drains
// any depth anyway. Fetch faster instead.
var (
	// chunkSize is the range each request asks for, and the memory cost: a track
	// holds at most the chunk being read, one queued and one in flight. Bigger is
	// faster, per-request latency dominating (256 KiB measured 1.1 MB/s against
	// 1 MiB's 2.4 MB/s); one chunk is about a minute of audio at YouTube's usual
	// bitrate. A var so tests can shrink it, the same seam playerEndpoint gives.
	chunkSize int64 = 1 << 20
	// prefetchChunks is how many fetched chunks may wait in the queue. One is
	// enough to keep a request always in flight, which is the point: a chunk
	// boundary took up to half a second to answer, and unpipelined that is a
	// half-second hole in playback.
	prefetchChunks = 1
	// maxChunkAttempts bounds retries of one range before the stream is declared
	// lost and RecoveryStream's reopen takes over; see fetchChunk for why the
	// retry lives here at all.
	maxChunkAttempts = 3
)

// errStreamClosed is what a read gets once Close has been called. Do not
// simplify this to io.EOF: RecoveryStream commits a cached track on a clean end
// and only on a clean end, so reporting EOF for a stop would commit a
// half-length blob as the whole track, and every later play would serve the
// truncated copy. See RecoveryStream.ReadPacket in stream/recovery.go.
var errStreamClosed = errors.New("ytnative: stream closed")

// chunkedBody reads a CDN file as a sequence of ranged requests, fetching the
// next chunk while the demuxer consumes the current one. It is an
// io.ReadCloser, so nothing above can tell it from a single response body. It
// requires a known total length; openCDNBody is where that is decided, and why.
type chunkedBody struct {
	client *http.Client
	url    string
	ua     string
	size   int64

	// ctx cancels a request in flight so a stop or a skip does not wait on a
	// chunk nobody will listen to. Close cancels it.
	ctx    context.Context
	cancel context.CancelFunc

	chunks chan []byte
	wg     sync.WaitGroup

	// cur and err belong to the reading goroutine. err is published to it by the
	// deferred close(chunks) in run, the same handshake opus.BufferedReader uses.
	cur []byte
	err error
}

// newChunkedBody starts prefetching after first, which the caller has already
// fetched as the opening range — see openCDNBody, where that request doubles as
// the probe deciding whether chunking is possible at all.
func newChunkedBody(client *http.Client, url, ua string, size int64, first []byte) *chunkedBody {
	ctx, cancel := context.WithCancel(context.Background())
	b := &chunkedBody{
		client: client,
		url:    url,
		ua:     ua,
		size:   size,
		ctx:    ctx,
		cancel: cancel,
		chunks: make(chan []byte, prefetchChunks),
		cur:    first,
	}
	b.wg.Add(1)
	go b.run(int64(len(first)))
	return b
}

// run is the prefetcher, owned by newChunkedBody and joined by Close. It exits
// on the file's end, on a chunk it could not get, or on ctx being cancelled;
// closing chunks is what hands its verdict to the reading goroutine.
func (b *chunkedBody) run(offset int64) {
	defer b.wg.Done()
	defer close(b.chunks)

	for offset < b.size {
		data, err := b.fetchChunk(offset)
		if err != nil {
			b.err = err
			return
		}
		if len(data) == 0 {
			// A range inside a file the CDN said was longer, answered with no
			// bytes: continuing would spin on the same offset forever.
			b.err = fmt.Errorf("ytnative: empty chunk at byte %d of %d", offset, b.size)
			return
		}
		select {
		case b.chunks <- data:
		case <-b.ctx.Done():
			// No error recorded here on purpose: Read is the single place that
			// tells a closed stream from a finished one, off ctx, and a second
			// opinion would only be one more thing to keep in agreement.
			return
		}
		offset += int64(len(data))
	}
}

// fetchChunk gets one range, retrying a failure in place. Retrying inside a
// parser is against the house rule that recovery belongs to RecoveryStream,
// and it is the same deliberate exception resume.go takes: a lost range costs
// one re-request, while letting it reach recovery costs a fresh player call
// plus re-fetching everything already heard, since a seek is served from byte
// zero. The budget is per chunk and does not accumulate, so a flaky link can
// retry all track long; what it stops is spinning on a dead source. Recovery
// upstairs stays the outer net for an expired URL or a video pulled mid-play.
func (b *chunkedBody) fetchChunk(offset int64) ([]byte, error) {
	end := offset + chunkSize - 1
	if end > b.size-1 {
		end = b.size - 1
	}

	for attempt := 1; ; attempt++ {
		data, err := b.getRange(offset, end)
		if err == nil {
			return data, nil
		}
		if b.ctx.Err() != nil {
			return nil, errStreamClosed
		}
		if attempt >= maxChunkAttempts {
			return nil, fmt.Errorf("ytnative: chunk at byte %d lost after %d attempts: %w", offset, attempt, err)
		}
		l := logger()
		l.Warn().Str("url_host", hostOf(b.url)).Int64("offset", offset).
			Int("attempt", attempt).Err(err).Msg("ytnative_chunk_retry")
		select {
		case <-time.After(resumeBackoff):
		case <-b.ctx.Done():
			return nil, errStreamClosed
		}
	}
}

func (b *chunkedBody) getRange(start, end int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(b.ctx, http.MethodGet, b.url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", b.ua)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	// 206 is the only answer that means "here is the range you asked for". A 200
	// is the whole file from byte zero, which at this offset continues nothing.
	if resp.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("ytnative: chunk: cdn %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func (b *chunkedBody) Read(p []byte) (int, error) {
	for len(b.cur) == 0 {
		data, ok := <-b.chunks
		if !ok {
			if b.err != nil {
				return 0, b.err
			}
			// A stream that was closed is not a stream that ended.
			if b.ctx.Err() != nil {
				return 0, errStreamClosed
			}
			return 0, io.EOF
		}
		b.cur = data
	}
	n := copy(p, b.cur)
	b.cur = b.cur[n:]
	return n, nil
}

// Close cancels whatever is in flight and waits for the prefetcher to exit, so
// teardown does not leave a chunk downloading for a track nobody is playing.
// Idempotent.
func (b *chunkedBody) Close() error {
	b.cancel()
	b.wg.Wait()
	return nil
}

// openCDNBody opens a format URL for reading, preferring chunked ranged
// fetching for the reason at the top of this file. The opening request doubles
// as the probe, so preferring chunks costs no extra round trip; anything the
// CDN will not serve as a bounded range of a stated length falls back to one
// open-ended response, repaired in place by resumingBody.
func openCDNBody(url string) (io.ReadCloser, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", clientUserAgent)
	req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", chunkSize-1))

	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, err
	}

	l := logger()
	switch resp.StatusCode {
	case http.StatusOK:
		// The range was ignored and the whole file is already streaming, so the
		// response in hand is exactly what the fallback wants.
		l.Info().Str("url_host", hostOf(url)).Str("reason", "range_ignored").
			Msg("ytnative_stream_unchunked")
		return newResumingBody(streamClient, url, clientUserAgent, resp.Body), nil
	case http.StatusPartialContent:
	default:
		_ = resp.Body.Close()
		return nil, fmt.Errorf("ytnative: cdn %s", resp.Status)
	}

	size, known := totalFromContentRange(resp.Header.Get("Content-Range"))
	if !known {
		// No stated length: nothing here can say where the file ends, and a
		// source that cannot say is one that may still be growing. Don't drop
		// this branch because YouTube VOD always states a length — it is what
		// keeps a live or otherwise open-ended source on the streaming path,
		// where its ending is discovered rather than assumed.
		_ = resp.Body.Close()
		l.Info().Str("url_host", hostOf(url)).Str("reason", "length_unknown").
			Msg("ytnative_stream_unchunked")
		return openStreamingBody(url)
	}

	first, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	l.Info().Str("url_host", hostOf(url)).Int64("size", size).
		Int64("chunk", chunkSize).Msg("ytnative_stream_chunked")
	return newChunkedBody(streamClient, url, clientUserAgent, size, first), nil
}

// openStreamingBody is the fallback shape: one open-ended response, with
// resumingBody repairing a dropped connection underneath the demuxer.
func openStreamingBody(url string) (io.ReadCloser, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", clientUserAgent)
	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("ytnative: cdn %s", resp.Status)
	}
	return newResumingBody(streamClient, url, clientUserAgent, resp.Body), nil
}

// totalFromContentRange reads the instance length off "bytes 0-1023/3433755".
// A "*" length — the CDN declining to say how long the file is — reports
// false, as does a header that isn't there at all.
func totalFromContentRange(v string) (int64, bool) {
	i := strings.LastIndex(v, "/")
	if i < 0 {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimSpace(v[i+1:]), 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}
