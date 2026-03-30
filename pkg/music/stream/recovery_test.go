package stream

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/sources"
)

type failLink struct{}

func (failLink) GetLinkStream(*parsers.TrackParse, float64) (io.ReadCloser, func(), error) {
	return nil, nil, errors.New("open failed")
}
func (failLink) GetPipeStream(*parsers.TrackParse, float64) (io.ReadCloser, func(), error) {
	return nil, nil, errors.New("open failed")
}
func (failLink) SupportsPipe() bool { return false }

type okLink struct{}

func (okLink) GetLinkStream(*parsers.TrackParse, float64) (io.ReadCloser, func(), error) {
	return io.NopCloser(bytes.NewReader(make([]byte, 1024))), func() {}, nil
}
func (okLink) GetPipeStream(*parsers.TrackParse, float64) (io.ReadCloser, func(), error) {
	return nil, nil, errors.New("not pipe")
}
func (okLink) SupportsPipe() bool { return false }

func TestRecoveryStream_Open_FailoverUpdatesCurrentParser(t *testing.T) {
	orig := StreamerRegistry
	t.Cleanup(func() { StreamerRegistry = orig })

	StreamerRegistry = map[string]parsers.Streamer{
		"fail-link": failLink{},
		"ok-link":   okLink{},
	}

	track := &parsers.TrackParse{
		Title:         "t",
		URL:           "https://example.com/x",
		CurrentParser: "fail-link",
		Duration:      time.Minute,
		SourceInfo: sources.TrackInfo{
			AvailableParsers: []string{"fail-link", "ok-link"},
		},
	}

	rs := NewRecoveryStream(track)
	if err := rs.Open(0); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if track.CurrentParser != "ok-link" {
		t.Fatalf("CurrentParser=%q want ok-link", track.CurrentParser)
	}
	if g := rs.GetParser(); g != "ok-link" {
		t.Fatalf("GetParser=%q want ok-link", g)
	}
	_ = rs.Close()
}

func TestRecoveryStream_ShouldRecover_UsesActiveStreamParser(t *testing.T) {
	orig := StreamerRegistry
	t.Cleanup(func() { StreamerRegistry = orig })
	StreamerRegistry = map[string]parsers.Streamer{
		"ok-link": okLink{},
	}

	track := &parsers.TrackParse{
		Title:         "t",
		URL:           "https://example.com/x",
		CurrentParser: "stale-wrong",
		Duration:      time.Minute,
		SourceInfo: sources.TrackInfo{
			AvailableParsers: []string{"ok-link"},
		},
	}
	rs := NewRecoveryStream(track)
	if err := rs.Open(0); err != nil {
		t.Fatal(err)
	}
	// Stream is on ok-link but track had stale CurrentParser until Open fixed it
	track.CurrentParser = "stale-wrong"
	rs.stream.Parser = "ok-link"

	if !rs.shouldRecover() {
		t.Fatal("expected recovery when stream parser has room under max retries")
	}
	_ = rs.Close()
}
