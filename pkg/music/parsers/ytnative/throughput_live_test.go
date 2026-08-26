package ytnative

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/keshon/melodix/pkg/music/opus"
)

// These tests measure how googlevideo actually delivers audio, because the
// anti-skip buffer's whole value depends on it: a lead can only build while the
// source outruns the 50 packets/s playback consumes. Opt-in via
// MELODIX_LIVE_TESTS=1; DIAG_VIDEO overrides the video.
//
// Measured 2026-08-26 against dQw4w9WgXcQ (itag 251, 136544 bps, 3.27 MiB):
//
//	open-ended GET      31.9 KiB/s   1.93x real time   whole file in 105s
//	1 MiB ranges      2020.7 KiB/s    121x real time   whole file in 1.66s
//
// The cap on an open-ended response is proportional, not absolute — every
// Opus format is paced at ~1.9x its own bitrate — so a lower format buys
// fewer bytes and no extra headroom. Ranged requests are not paced at all.

func liveFormat(t *testing.T) (format, *playerResponse) {
	t.Helper()
	if os.Getenv("MELODIX_LIVE_TESTS") == "" {
		t.Skip("set MELODIX_LIVE_TESTS=1 to hit real YouTube")
	}
	id := os.Getenv("DIAG_VIDEO")
	if id == "" {
		id = "dQw4w9WgXcQ"
	}
	pr, err := fetchPlayer(httpClient, playerEndpoint, id)
	if err != nil {
		t.Fatalf("fetchPlayer: %v", err)
	}
	f, ok := pickOpusFormat(pr.StreamingData.AdaptiveFormats)
	if !ok {
		t.Fatal("no opus/webm format offered")
	}
	return f, pr
}

// TestLiveThroughput reports whether the passthrough reader gets ahead of
// playback. Read the lead column: it climbs by roughly the delivery rate minus
// one, so at 1.9x it gains about a second per second — meaning the configured
// buffer depth is only reached half a minute into the track, and the opening
// seconds ride on almost no reserve.
func TestLiveThroughput(t *testing.T) {
	f, _ := liveFormat(t)
	t.Logf("format %s @ %d bps (%.1f KiB per second of audio)",
		f.MimeType, f.Bitrate, float64(f.Bitrate)/8/1024)

	r, cleanup, err := openPassthrough(f.URL, 0)
	if err != nil {
		t.Fatalf("openPassthrough: %v", err)
	}
	defer cleanup()

	start := time.Now()
	var packets, second int
	var worstGap time.Duration
	prev := start
	for time.Since(start) < 20*time.Second {
		if _, err := r.ReadPacket(); err != nil {
			t.Logf("stream ended after %d packets: %v", packets, err)
			break
		}
		now := time.Now()
		if gap := now.Sub(prev); gap > worstGap {
			worstGap = gap
		}
		prev = now
		packets++
		if s := int(now.Sub(start) / time.Second); s > second {
			second = s
			audio := float64(packets*opus.FrameMs) / 1000
			t.Logf("t=%2ds packets=%6d audio=%6.1fs lead=%+6.1fs", s, packets, audio, audio-now.Sub(start).Seconds())
		}
	}
	t.Logf("worst gap between two packets: %v (playback needs one every 20ms)", worstGap)
}

// TestLiveFetchShape contrasts the open-ended GET the parser issues today with
// sequential ranged chunks over the same file. The gap between the two is the
// entire argument for fetching in chunks.
func TestLiveFetchShape(t *testing.T) {
	f, pr := liveFormat(t)
	secs, _ := strconv.Atoi(pr.VideoDetails.LengthSeconds)
	size := formatSize(t, f.URL)
	t.Logf("track %q  %ds  %d bps  %.2f MiB", pr.VideoDetails.Title, secs, f.Bitrate, float64(size)/(1<<20))

	openEndedRate(t, f.URL, 20*time.Second, f.Bitrate)
	for _, chunk := range []int64{256 << 10, 1 << 20, 4 << 20} {
		chunkedRate(t, f.URL, size, chunk)
	}
}

// TestLiveCapShape asks whether the open-ended cap is an absolute byte rate or
// a multiple of the format's own bitrate — i.e. whether MAX_AUDIO_BITRATE buys
// headroom against the CDN or merely fewer bytes over the link.
func TestLiveCapShape(t *testing.T) {
	_, pr := liveFormat(t)
	for _, f := range pr.StreamingData.AdaptiveFormats {
		if f.URL == "" || !strings.Contains(f.MimeType, "opus") {
			continue
		}
		openEndedRate(t, f.URL, 8*time.Second, f.Bitrate)
	}
}

// formatSize reads the total length off a one-byte ranged request's
// Content-Range header.
func formatSize(t *testing.T, url string) int64 {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("User-Agent", clientUserAgent)
	req.Header.Set("Range", "bytes=0-0")
	resp, err := streamClient.Do(req)
	if err != nil {
		t.Fatalf("format size: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	cr := resp.Header.Get("Content-Range") // "bytes 0-0/3430711"
	slash := strings.LastIndex(cr, "/")
	if slash < 0 {
		t.Fatalf("no content-range: %q", cr)
	}
	n, err := strconv.ParseInt(cr[slash+1:], 10, 64)
	if err != nil {
		t.Fatalf("content-range %q: %v", cr, err)
	}
	return n
}

func openEndedRate(t *testing.T, url string, d time.Duration, bitrate int) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("User-Agent", clientUserAgent)
	resp, err := streamClient.Do(req)
	if err != nil {
		t.Logf("open-ended %d bps: %v", bitrate, err)
		return
	}
	defer resp.Body.Close()

	buf := make([]byte, 64<<10)
	start := time.Now()
	var total int64
	for time.Since(start) < d {
		n, rerr := resp.Body.Read(buf)
		total += int64(n)
		if rerr != nil {
			break
		}
	}
	rate := float64(total) / time.Since(start).Seconds()
	t.Logf("open-ended  %6d bps format -> %7.1f KiB/s  (%.2fx real time)",
		bitrate, rate/1024, rate*8/float64(bitrate))
}

func chunkedRate(t *testing.T, url string, size, chunk int64) {
	t.Helper()
	buf := make([]byte, 64<<10)
	start := time.Now()
	var got int64
	var slowest time.Duration
	var reqs int
	for off := int64(0); off < size; off += chunk {
		end := off + chunk - 1
		if end >= size {
			end = size - 1
		}
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("User-Agent", clientUserAgent)
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", off, end))
		t0 := time.Now()
		resp, err := streamClient.Do(req)
		if err != nil {
			t.Logf("chunk at %d: %v", off, err)
			return
		}
		if resp.StatusCode != http.StatusPartialContent {
			_ = resp.Body.Close()
			t.Logf("chunk at %d: status %s", off, resp.Status)
			return
		}
		for {
			n, rerr := resp.Body.Read(buf)
			got += int64(n)
			if rerr != nil {
				break
			}
		}
		_ = resp.Body.Close()
		reqs++
		if el := time.Since(t0); el > slowest {
			slowest = el
		}
	}
	el := time.Since(start).Seconds()
	t.Logf("chunked   %5d KiB ranges -> %7.1f KiB/s  %.2fs for %.2f MiB in %d reqs (slowest %v)",
		chunk/1024, float64(got)/1024/el, el, float64(got)/(1<<20), reqs, slowest)
}
