package sink

import (
	"io"
	"sync"
	"time"

	"github.com/ebitengine/oto/v3"
	"github.com/keshon/melodix/pkg/music/opus"
	"github.com/keshon/melodix/pkg/music/stream"
	"github.com/rs/zerolog"
)

// SpeakerSink decodes a track's Opus packets to PCM (48kHz, 2ch, 16-bit LE) and
// plays them to the default audio device.
type SpeakerSink struct {
	ctx       *oto.Context
	readyChan <-chan struct{}
	contextMu sync.Mutex
	log       zerolog.Logger
}

// NewSpeakerSink creates a new speaker sink. The oto context is created lazily on first Stream().
func NewSpeakerSink() *SpeakerSink {
	return NewSpeakerSinkWithLogger(zerolog.Nop())
}

// NewSpeakerSinkWithLogger creates a new speaker sink with optional logging.
func NewSpeakerSinkWithLogger(log zerolog.Logger) *SpeakerSink {
	if log.GetLevel() == zerolog.NoLevel {
		log = zerolog.Nop()
	}
	return &SpeakerSink{log: log}
}

// ensureContext creates the oto context once.
func (s *SpeakerSink) ensureContext() error {
	s.contextMu.Lock()
	defer s.contextMu.Unlock()
	if s.ctx != nil {
		return nil
	}
	op := &oto.NewContextOptions{
		SampleRate:   opus.SampleRate,
		ChannelCount: opus.Channels,
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

// Stream decodes the Opus packets to PCM and plays them. Returns when the stream
// ends or stop is closed.
func (s *SpeakerSink) Stream(r opus.Reader, stop <-chan struct{}) error {
	if err := s.ensureContext(); err != nil {
		return err
	}
	<-s.readyChan

	pcm := opus.DecodeReader(r)
	defer pcm.Close()
	sr := &speakerStopReader{r: pcm, stop: stop}
	player := s.ctx.NewPlayer(sr)
	player.Play()

	for player.IsPlaying() {
		select {
		case <-stop:
			return stream.ErrPlaybackStopped
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	return nil
}

// speakerStopReader wraps a reader and makes Read return (0, io.EOF) when stop is closed.
type speakerStopReader struct {
	r    io.Reader
	stop <-chan struct{}
}

func (s *speakerStopReader) Read(p []byte) (n int, err error) {
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
			s.log.Warn().Err(err).Msg("speaker_suspend_failed")
		}
		s.ctx = nil
	}
	return nil
}
