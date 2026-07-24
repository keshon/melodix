package ffmpeg

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
)

// ProcessStream represents a streaming reader backed by an external process.
//
// It reads from the process stdout and tracks its lifecycle. When the underlying
// process exits, Read() inspects the exit status:
//
//   - If the process exited successfully, io.EOF is returned as-is.
//   - If the process exited with an error, a "silent" io.EOF is converted into
//     that process error (especially important when no data was produced).
//
// This avoids false-positive EOFs in cases where the process fails immediately
// (e.g. invalid input, network errors) and ensures callers can distinguish
// between a natural stream end and a failure.
//
// Close() terminates the process and waits for it to exit.
type ProcessStream struct {
	cmd *exec.Cmd

	stdout io.ReadCloser

	waitOnce sync.Once
	waitErr  error
	done     chan struct{}
}

// NewProcessStream wraps a started command and its stdout; it begins waiting
// for process exit immediately.
func NewProcessStream(cmd *exec.Cmd, stdout io.ReadCloser) *ProcessStream {
	ps := &ProcessStream{
		cmd:    cmd,
		stdout: stdout,
		done:   make(chan struct{}),
	}

	go func() {
		ps.waitOnce.Do(func() {
			ps.waitErr = cmd.Wait()
		})
		close(ps.done)
	}()

	return ps
}

func (p *ProcessStream) Read(b []byte) (int, error) {
	n, err := p.stdout.Read(b)

	// io.EOF is a natural stream end; os.ErrClosed ("file already closed") is the
	// same end seen through a race — cmd.Wait() closes the stdout pipe when the
	// process exits, which can beat the final Read. Both mean the process is done,
	// so wait for the exit status and classify by it.
	if err == io.EOF || errors.Is(err, os.ErrClosed) {
		<-p.done
		return classifyExit(n, p.waitErr)
	}

	return n, err
}

// classifyExit maps a terminal stdout read (EOF or a closed pipe) to clean-end
// or process-error semantics: a failed exit with no data surfaces the error (so
// recovery can act on it); everything else is a clean io.EOF.
func classifyExit(n int, waitErr error) (int, error) {
	if waitErr != nil {
		if n == 0 {
			return 0, waitErr
		}
		return n, nil
	}
	return n, io.EOF
}

func (p *ProcessStream) Close() error {
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}

	<-p.done

	return p.stdout.Close()
}

// WaitErr blocks until the process exits and returns its exit error, if any.
func (p *ProcessStream) WaitErr() error {
	<-p.done
	return p.waitErr
}
