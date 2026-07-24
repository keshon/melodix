package opus

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

// bufSrc yields the given packets in order, then termErr (default io.EOF), and
// counts Close calls.
type bufSrc struct {
	pkts    [][]byte
	i       int
	termErr error
	closed  int
}

func (s *bufSrc) ReadPacket() ([]byte, error) {
	if s.i >= len(s.pkts) {
		if s.termErr != nil {
			return nil, s.termErr
		}
		return nil, io.EOF
	}
	p := s.pkts[s.i]
	s.i++
	return p, nil
}

func (s *bufSrc) Close() error { s.closed++; return nil }

func drainBuf(t *testing.T, r Reader) ([][]byte, error) {
	t.Helper()
	var got [][]byte
	for {
		pkt, err := r.ReadPacket()
		if err != nil {
			return got, err
		}
		got = append(got, pkt)
	}
}

func TestBufferedReaderOrderAndEOF(t *testing.T) {
	want := [][]byte{{1}, {2}, {3}, {4, 5}}
	b := NewBufferedReader(&bufSrc{pkts: want}, 2)
	got, err := drainBuf(t, b)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("terminal error = %v, want io.EOF", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d packets, want %d", len(got), len(want))
	}
	for i := range want {
		if !bytes.Equal(got[i], want[i]) {
			t.Fatalf("packet %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestBufferedReaderErrorAfterDrain(t *testing.T) {
	boom := errors.New("boom")
	b := NewBufferedReader(&bufSrc{pkts: [][]byte{{1}, {2}}, termErr: boom}, 4)
	got, err := drainBuf(t, b)
	if !errors.Is(err, boom) {
		t.Fatalf("terminal error = %v, want boom", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d packets before error, want 2 (error must surface only after drain)", len(got))
	}
}

func TestBufferedReaderStopDoesNotCloseSource(t *testing.T) {
	src := &bufSrc{pkts: [][]byte{{1}, {2}, {3}, {4}, {5}}}
	b := NewBufferedReader(src, 2).(*BufferedReader)

	b.Stop()
	if src.closed != 0 {
		t.Fatalf("Stop closed the source (closed=%d), want 0", src.closed)
	}

	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if src.closed != 1 {
		t.Fatalf("Close closed source %d times, want 1", src.closed)
	}
	b.Stop() // idempotent, must not panic
}

func TestBufferedReaderZeroDepthPassthrough(t *testing.T) {
	src := &bufSrc{pkts: [][]byte{{1}}}
	if got := NewBufferedReader(src, 0); got != Reader(src) {
		t.Fatalf("depth<=0 should return the source unchanged")
	}
}
