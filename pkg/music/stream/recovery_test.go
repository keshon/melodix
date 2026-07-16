package stream

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/sources"
)

type fakeStreamer struct {
	link func(track *parsers.Track, seekSec float64) (io.ReadCloser, func(), error)
}

func (s fakeStreamer) LinkStream(track *parsers.Track, seekSec float64) (io.ReadCloser, func(), error) {
	return s.link(track, seekSec)
}
func (s fakeStreamer) PipeStream(track *parsers.Track, seekSec float64) (io.ReadCloser, func(), error) {
	return nil, nil, io.ErrUnexpectedEOF
}

type eofOnFirstRead struct {
	read bool
}

func (r *eofOnFirstRead) Read(p []byte) (int, error) {
	if r.read {
		return 0, io.EOF
	}
	r.read = true
	return 0, io.EOF
}
func (r *eofOnFirstRead) Close() error { return nil }

func TestRecoveryStream_ImmediateFail_SwitchesToNextParser(t *testing.T) {
	origRegistry := Registry
	Registry = map[string]Entry{}
	defer func() { Registry = origRegistry }()

	Registry["p1"] = Entry{Streamer: fakeStreamer{
		link: func(track *parsers.Track, seekSec float64) (io.ReadCloser, func(), error) {
			return &eofOnFirstRead{}, func() {}, nil
		},
	}}
	Registry["p2"] = Entry{Streamer: fakeStreamer{
		link: func(track *parsers.Track, seekSec float64) (io.ReadCloser, func(), error) {
			return io.NopCloser(bytes.NewReader([]byte("ok"))), func() {}, nil
		},
	}}

	track := &parsers.Track{
		SourceInfo: sources.TrackInfo{AvailableParsers: []string{"p1", "p2"}},
	}
	rs := NewRecoveryStream(track)
	if err := rs.Open(0); err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	buf := make([]byte, 16)
	n, err := rs.Read(buf)
	if err != nil {
		t.Fatalf("Read returned error after recovery: %v", err)
	}
	if n == 0 || string(buf[:n]) != "ok" {
		t.Fatalf("expected to read from parser2, got n=%d data=%q", n, string(buf[:n]))
	}
	if track.CurrentParser != "p2" {
		t.Fatalf("expected CurrentParser to switch to p2, got %q", track.CurrentParser)
	}
}

func TestRecoveryStream_NaturalEOF_DoesNotFallback(t *testing.T) {
	origRegistry := Registry
	Registry = map[string]Entry{}
	defer func() { Registry = origRegistry }()

	Registry["p1"] = Entry{Streamer: fakeStreamer{
		link: func(track *parsers.Track, seekSec float64) (io.ReadCloser, func(), error) {
			// One successful read then EOF.
			return io.NopCloser(bytes.NewReader([]byte("data"))), func() {}, nil
		},
	}}
	Registry["p2"] = Entry{Streamer: fakeStreamer{
		link: func(track *parsers.Track, seekSec float64) (io.ReadCloser, func(), error) {
			return io.NopCloser(bytes.NewReader([]byte("fallback"))), func() {}, nil
		},
	}}

	track := &parsers.Track{
		Duration:   1 * time.Microsecond, // tiny, so EOF will be treated as natural end
		SourceInfo: sources.TrackInfo{AvailableParsers: []string{"p1", "p2"}},
	}
	rs := NewRecoveryStream(track)
	if err := rs.Open(0); err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	buf := make([]byte, 16)
	_, err := rs.Read(buf)
	if err != nil {
		t.Fatalf("first read should succeed, got: %v", err)
	}

	// Drain to EOF: should NOT attempt recovery / fallback.
	for {
		_, err = rs.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error while draining: %v", err)
		}
	}

	if track.CurrentParser != "p1" {
		t.Fatalf("expected to stay on p1, got %q", track.CurrentParser)
	}
	if rs.parserIndex != 0 {
		t.Fatalf("expected parserIndex to remain 0, got %d", rs.parserIndex)
	}
}
