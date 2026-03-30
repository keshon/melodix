package player

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/sink"
	"github.com/keshon/melodix/pkg/music/sources"
	"github.com/keshon/melodix/pkg/music/stream"
)

type fakeResolver struct {
	out []sources.TrackInfo
	err error
}

func (f *fakeResolver) Resolve(_ context.Context, _, _, _ string) ([]sources.TrackInfo, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.out, nil
}

type discardSink struct{}

func (discardSink) Stream(r io.ReadCloser, stop <-chan struct{}) error {
	defer r.Close()
	buf := make([]byte, 8192)
	for {
		select {
		case <-stop:
			return stream.ErrPlaybackStopped
		default:
		}
		n, err := r.Read(buf)
		if errors.Is(err, io.EOF) || (err == nil && n == 0) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

type fakeSinkProvider struct{}

func (fakeSinkProvider) GetSink(string) (sink.AudioSink, error) { return discardSink{}, nil }
func (fakeSinkProvider) ReleaseSink(string)                     {}

type smokeLink struct{}

func (smokeLink) GetLinkStream(*parsers.TrackParse, float64) (io.ReadCloser, func(), error) {
	// A few frames of PCM so RecoveryStream opens and runPlayback drains without hanging.
	return io.NopCloser(bytes.NewReader(make([]byte, stream.FrameSize*stream.Channels*2*8))), func() {}, nil
}
func (smokeLink) GetPipeStream(*parsers.TrackParse, float64) (io.ReadCloser, func(), error) {
	return nil, nil, errors.New("not used")
}
func (smokeLink) SupportsPipe() bool { return false }

func TestPlayer_EnqueueResolve(t *testing.T) {
	p := New(fakeSinkProvider{}, &fakeResolver{
		out: []sources.TrackInfo{{
			URL:              "https://example.com/a",
			Title:            "A",
			AvailableParsers: []string{"smoke-link"},
		}},
	})
	if err := p.Enqueue(context.Background(), "https://example.com/a", "", ""); err != nil {
		t.Fatal(err)
	}
	q := p.Queue()
	if len(q) != 1 || q[0].Title != "A" {
		t.Fatalf("queue: %+v", q)
	}
}

func TestPlayer_PlayNext_Smoke(t *testing.T) {
	orig := stream.StreamerRegistry
	t.Cleanup(func() { stream.StreamerRegistry = orig })
	stream.StreamerRegistry = map[string]parsers.Streamer{
		"smoke-link": smokeLink{},
	}

	p := New(fakeSinkProvider{}, &fakeResolver{
		out: []sources.TrackInfo{{
			URL:              "https://example.com/smoke",
			Title:            "Smoke",
			AvailableParsers: []string{"smoke-link"},
		}},
	})

	if err := p.Enqueue(context.Background(), "x", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := p.PlayNext(""); err != nil {
		t.Fatal(err)
	}

	select {
	case st := <-p.PlayerStatus:
		if st != StatusPlaying {
			t.Fatalf("status: %s", st)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for StatusPlaying")
	}

	deadline := time.After(5 * time.Second)
	for p.IsPlaying() {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for playback to finish")
		default:
		}
		time.Sleep(5 * time.Millisecond)
	}
}
