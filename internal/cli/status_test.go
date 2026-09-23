package cli

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/music"
	"github.com/keshon/melodix/pkg/music/player"
	"github.com/keshon/melodix/pkg/music/sink"
	"github.com/rs/zerolog"
)

type noSink struct{}

func (noSink) Sink(string) (sink.AudioSink, error) { return nil, errors.New("no audio in tests") }
func (noSink) ReleaseSink(string)                  {}
func (noSink) InvalidateSink()                     {}

type lockedWriter struct {
	mu sync.Mutex
	b  strings.Builder
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}

func (w *lockedWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.String()
}

// Quitting stops the player, and Stop announces Stopped whether or not
// anything was playing, so an idle CLI used to sign off with "Playback
// finished" for music it never played.
func TestStatusPrinterAnnouncesOnlyAFinishItSawStart(t *testing.T) {
	svc := music.New(&config.Config{}, nil, zerolog.Nop(), music.Hooks{
		NewSink: func(string) sink.Provider { return noSink{} },
	})
	p := svc.Player("cli")
	out := &lockedWriter{}
	go StatusPrinter(out)("cli", p)

	p.PlayerStatus <- player.StatusStopped // idle: nothing started
	p.PlayerStatus <- player.StatusPlaying
	p.PlayerStatus <- player.StatusStopped // the queue ran out

	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(out.String(), "finished") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := strings.Count(out.String(), "Playback finished"); n != 1 {
		t.Fatalf("announced a finish %d times, want once:\n%s", n, out.String())
	}
}
