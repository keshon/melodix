package voicesink

import (
	"errors"
	"log/slog"
	"testing"

	"github.com/disgoorg/disgo/voice"
	"github.com/keshon/melodix/pkg/music/stream"
)

// Like Disgo 0.20, closing a sender also closes its provider. Open is
// synchronous here so the test isolates provider ownership from its clock.
type trackSwitchSender struct {
	provider voice.OpusFrameProvider
}

func (s *trackSwitchSender) Open() {}
func (s *trackSwitchSender) Close() {
	s.provider.Close()
}

func TestConsecutiveTracksKeepAValidSenderProvider(t *testing.T) {
	var providers []voice.OpusFrameProvider
	conn := voice.NewConn(testGuild, 2, nil, func() {},
		voice.WithConnAudioSenderCreateFunc(func(_ *slog.Logger, provider voice.OpusFrameProvider, _ voice.Conn) voice.AudioSender {
			providers = append(providers, provider)
			return &trackSwitchSender{provider: provider}
		}),
	)

	for range 2 {
		stop := make(chan struct{})
		close(stop)
		err := newTestSink(conn, nil).Stream(&endlessAudio{}, stop)
		if !errors.Is(err, stream.ErrPlaybackStopped) {
			t.Fatalf("stopping track: %v", err)
		}
	}

	if len(providers) != 2 {
		t.Fatalf("created %d senders for two tracks, want 2", len(providers))
	}
	for _, provider := range providers {
		frame, err := provider.ProvideOpusFrame()
		if len(frame) != 0 || err == nil {
			t.Fatalf("stopped track still supplies audio: frame=%v, err=%v", frame, err)
		}
	}
}
