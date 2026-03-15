package speaker

import (
	"io"
	"log"
	"sync"
	"time"

	"github.com/ebitengine/oto/v3"
	"github.com/keshon/melodix/pkg/music/stream"
)

// SpeakerSink plays PCM (48kHz, 2ch, 16-bit LE) to the default audio device.
type SpeakerSink struct {
	ctx        *oto.Context
	readyChan  <-chan struct{}
	contextMu  sync.Mutex
}

// NewSpeakerSink creates a new speaker sink. The oto context is created lazily on first Stream().
func NewSpeakerSink() *SpeakerSink {
	return &SpeakerSink{}
}

// ensureContext creates the oto context once.
func (s *SpeakerSink) ensureContext() error {
	s.contextMu.Lock()
	defer s.contextMu.Unlock()
	if s.ctx != nil {
		return nil
	}
	op := &oto.NewContextOptions{
		SampleRate:   stream.SampleRate,
		ChannelCount: stream.Channels,
		Format:       oto.FormatSignedInt16LE,
	}
	ctx, ready, err := oto.NewContext(op)
	if err != nil {
		return err
	}
	s.ctx = ctx
	s.readyChan = ready
	return nil
}

// Stream reads PCM from the stream and plays it. Returns when the stream ends or stop is closed.
func (s *SpeakerSink) Stream(stream io.ReadCloser, stop <-chan struct{}) error {
	defer stream.Close()
	if err := s.ensureContext(); err != nil {
		return err
	}
	<-s.readyChan

	r := &stopReader{r: stream, stop: stop}
	player := s.ctx.NewPlayer(r)
	player.Play()

	for player.IsPlaying() {
		select {
		case <-stop:
			_ = player.Close()
			return nil
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	_ = player.Close()
	return nil
}

// stopReader wraps a reader and makes Read return (0, io.EOF) when stop is closed.
type stopReader struct {
	r    io.Reader
	stop <-chan struct{}
}

func (s *stopReader) Read(p []byte) (n int, err error) {
	select {
	case <-s.stop:
		return 0, io.EOF
	default:
	}
	n, err = s.r.Read(p)
	if err != nil {
		return n, err
	}
	select {
	case <-s.stop:
		return n, io.EOF
	default:
		return n, nil
	}
}

// Close releases the oto context. Call when the CLI exits to free the audio device.
func (s *SpeakerSink) Close() error {
	s.contextMu.Lock()
	defer s.contextMu.Unlock()
	if s.ctx != nil {
		err := s.ctx.Suspend()
		if err != nil {
			log.Printf("[SpeakerSink] Suspend: %v", err)
		}
		s.ctx = nil
	}
	return nil
}
