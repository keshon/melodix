package scnative

import (
	"fmt"
	"io"
	"time"

	"github.com/keshon/melodix/pkg/music/parsers"
	ffmpegparser "github.com/keshon/melodix/pkg/music/parsers/ffmpeg"
	"github.com/keshon/melodix/pkg/music/soundcloudapi"
)

func scnativeLink(track *parsers.Track, seekSec float64) (io.ReadCloser, func(), error) {
	sc := soundcloudapi.Default()

	t, err := sc.ResolveTrack(track.URL)
	if err != nil {
		return nil, nil, fmt.Errorf("scnative: resolve: %w", err)
	}
	track.Title = t.Title
	track.Duration = time.Duration(t.DurationMS) * time.Millisecond

	transcoding, err := soundcloudapi.PickTranscoding(t.Media.Transcodings)
	if err != nil {
		return nil, nil, fmt.Errorf("scnative: %w", err)
	}
	streamURL, err := sc.StreamURL(transcoding)
	if err != nil {
		return nil, nil, fmt.Errorf("scnative: stream url: %w", err)
	}

	// HLS input: ffmpeg's hls demuxer manages segment fetching itself, so the
	// reconnect flags only apply to single-HTTP-stream (progressive) inputs.
	reconnect := transcoding.Format.Protocol == "progressive"
	cmd := ffmpegparser.NewPCMCommand(streamURL, seekSec, reconnect, "scnative-link")

	reader, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("scnative: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("scnative: ffmpeg start: %w", err)
	}

	pr := ffmpegparser.NewProcessStream(cmd, reader)
	cleanup := func() {
		_ = pr.Close()
	}
	return pr, cleanup, nil
}
