package stream

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/keshon/melodix/pkg/music/opus"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/sources"
)

type fakeStreamer struct {
	open func(track *parsers.Track, seek float64) (opus.Reader, func(), error)
}

func (s fakeStreamer) Open(track *parsers.Track, seek float64) (opus.Reader, func(), error) {
	return s.open(track, seek)
}

// pktReader yields the given packets, then io.EOF.
type pktReader struct {
	pkts [][]byte
	i    int
}

func (r *pktReader) ReadPacket() ([]byte, error) {
	if r.i >= len(r.pkts) {
		return nil, io.EOF
	}
	p := r.pkts[r.i]
	r.i++
	return p, nil
}
func (r *pktReader) Close() error { return nil }

// errFirst fails immediately on the first ReadPacket (instant fail).
type errFirst struct{}

func (errFirst) ReadPacket() ([]byte, error) { return nil, io.EOF }
func (errFirst) Close() error                { return nil }

func TestRecoveryStream_ImmediateFail_SwitchesToNextParser(t *testing.T) {
	orig := SetRegistry(map[string]parsers.Streamer{
		"p1": fakeStreamer{open: func(*parsers.Track, float64) (opus.Reader, func(), error) {
			return errFirst{}, func() {}, nil
		}},
		"p2": fakeStreamer{open: func(*parsers.Track, float64) (opus.Reader, func(), error) {
			return &pktReader{pkts: [][]byte{{0xAA}}}, func() {}, nil
		}},
	})
	defer func() { SetRegistry(orig) }()

	track := &parsers.Track{SourceInfo: sources.TrackInfo{AvailableParsers: []string{"p1", "p2"}}}
	rs := NewRecoveryStream(track)
	if err := rs.Open(0); err != nil {
		t.Fatalf("Open: %v", err)
	}

	pkt, err := rs.ReadPacket()
	if err != nil {
		t.Fatalf("ReadPacket after recovery: %v", err)
	}
	if len(pkt) != 1 || pkt[0] != 0xAA {
		t.Fatalf("expected p2's packet, got %v", pkt)
	}
	if track.CurrentParser != "p2" {
		t.Fatalf("CurrentParser = %q, want p2", track.CurrentParser)
	}
}

func TestRecoveryStream_NaturalEOF_DoesNotFallback(t *testing.T) {
	orig := SetRegistry(map[string]parsers.Streamer{
		"p1": fakeStreamer{open: func(*parsers.Track, float64) (opus.Reader, func(), error) {
			return &pktReader{pkts: [][]byte{{0xAA}}}, func() {}, nil
		}},
		"p2": fakeStreamer{open: func(*parsers.Track, float64) (opus.Reader, func(), error) {
			return &pktReader{pkts: [][]byte{{0xBB}}}, func() {}, nil
		}},
	})
	defer func() { SetRegistry(orig) }()

	track := &parsers.Track{
		Duration:   1 * time.Microsecond, // tiny → EOF is a natural end
		SourceInfo: sources.TrackInfo{AvailableParsers: []string{"p1", "p2"}},
	}
	rs := NewRecoveryStream(track)
	if err := rs.Open(0); err != nil {
		t.Fatalf("Open: %v", err)
	}

	if _, err := rs.ReadPacket(); err != nil {
		t.Fatalf("first ReadPacket: %v", err)
	}
	if _, err := rs.ReadPacket(); !errors.Is(err, io.EOF) {
		t.Fatalf("second ReadPacket = %v, want EOF (no fallback)", err)
	}
	if track.CurrentParser != "p1" {
		t.Fatalf("stayed off p1: %q", track.CurrentParser)
	}
	if rs.parserIndex != 0 {
		t.Fatalf("parserIndex = %d, want 0", rs.parserIndex)
	}
}
