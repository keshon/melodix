package ffmpeg

import (
	"io"
	"os/exec"
	"sync"
)

// ProcessReadCloser wraps a process stdout pipe and turns a "silent" io.EOF into
// a process error when the command exits non-zero.
//
// Motivation: ffmpeg often fails immediately (e.g. 403) and closes stdout, so the
// first Read() returns io.EOF. Callers must see an error to trigger recovery.
type ProcessReadCloser struct {
	r   io.ReadCloser
	cmd *exec.Cmd

	waitOnce sync.Once
	waitErr  error
	done     chan struct{}
}

func NewProcessReadCloser(cmd *exec.Cmd, stdout io.ReadCloser) *ProcessReadCloser {
	p := &ProcessReadCloser{
		r:    stdout,
		cmd:  cmd,
		done: make(chan struct{}),
	}
	go func() {
		p.waitOnce.Do(func() { p.waitErr = cmd.Wait() })
		close(p.done)
	}()
	return p
}

func (p *ProcessReadCloser) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if err == io.EOF {
		<-p.done
		if p.waitErr != nil {
			return n, p.waitErr
		}
	}
	return n, err
}

func (p *ProcessReadCloser) Close() error {
	return p.r.Close()
}

func (p *ProcessReadCloser) WaitErr() error {
	<-p.done
	return p.waitErr
}
