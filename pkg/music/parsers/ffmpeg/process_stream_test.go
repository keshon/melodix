package ffmpeg

import (
	"errors"
	"io"
	"os"
	"testing"
)

// fakeStdout yields one data chunk (if any), then always returns endErr.
type fakeStdout struct {
	data   []byte
	served bool
	endErr error
}

func (f *fakeStdout) Read(b []byte) (int, error) {
	if !f.served && len(f.data) > 0 {
		f.served = true
		return copy(b, f.data), nil
	}
	return 0, f.endErr
}
func (f *fakeStdout) Close() error { return nil }

// exitedStream builds a ProcessStream whose process has already exited with
// waitErr, bypassing NewProcessStream's cmd.Wait goroutine.
func exitedStream(stdout io.ReadCloser, waitErr error) *ProcessStream {
	done := make(chan struct{})
	close(done)
	return &ProcessStream{stdout: stdout, done: done, waitErr: waitErr}
}

func TestProcessStream_ClosedPipeCleanExitIsEOF(t *testing.T) {
	// The core bug: ffmpeg finished cleanly (waitErr == nil) but cmd.Wait closed
	// the pipe, so the final Read sees os.ErrClosed. It must read as io.EOF.
	ps := exitedStream(&fakeStdout{data: []byte{1, 2, 3}, endErr: os.ErrClosed}, nil)

	b := make([]byte, 8)
	if n, err := ps.Read(b); err != nil || n != 3 {
		t.Fatalf("first read = (%d,%v), want (3,nil)", n, err)
	}
	if n, err := ps.Read(b); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("closed-pipe read after clean exit = (%d,%v), want (0,EOF)", n, err)
	}
}

func TestProcessStream_ClosedPipeFailedExitSurfacesError(t *testing.T) {
	boom := errors.New("exit status 1")
	ps := exitedStream(&fakeStdout{endErr: os.ErrClosed}, boom)

	if n, err := ps.Read(make([]byte, 8)); n != 0 || !errors.Is(err, boom) {
		t.Fatalf("closed-pipe read after failed exit = (%d,%v), want (0,boom)", n, err)
	}
}

func TestProcessStream_EOFCleanExitIsEOF(t *testing.T) {
	ps := exitedStream(&fakeStdout{endErr: io.EOF}, nil)
	if _, err := ps.Read(make([]byte, 8)); !errors.Is(err, io.EOF) {
		t.Fatalf("EOF read after clean exit = %v, want EOF", err)
	}
}

func TestProcessStream_MidStreamNonClosedErrorPassesThrough(t *testing.T) {
	// A non-terminal read error (not EOF/closed) is not a process end and must
	// pass through unchanged, without consulting exit status.
	other := errors.New("connection reset")
	ps := exitedStream(&fakeStdout{endErr: other}, nil)
	if _, err := ps.Read(make([]byte, 8)); !errors.Is(err, other) {
		t.Fatalf("non-terminal error = %v, want it passed through", err)
	}
}
