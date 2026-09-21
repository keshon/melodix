package scnative

import (
	"fmt"
	"time"

	"github.com/keshon/melodix/pkg/music/parsers"
	ffmpegparser "github.com/keshon/melodix/pkg/music/parsers/ffmpeg"
	"github.com/keshon/melodix/pkg/music/soundcloudapi"
)

func scnativeLink(track parsers.Track, seekSec float64) (parsers.Opened, error) {
	sc := soundcloudapi.Default()

	t, err := sc.ResolveTrack(track.URL)
	if err != nil {
		return parsers.Opened{}, fmt.Errorf("scnative: resolve: %w", err)
	}

	transcoding, err := soundcloudapi.PickTranscoding(t.Media.Transcodings)
	if err != nil {
		return parsers.Opened{}, fmt.Errorf("scnative: %w", err)
	}
	streamURL, err := sc.StreamURL(transcoding)
	if err != nil {
		return parsers.Opened{}, fmt.Errorf("scnative: stream url: %w", err)
	}

	// HLS input: ffmpeg's hls demuxer manages segment fetching itself, so the
	// reconnect flags only apply to single-HTTP-stream (progressive) inputs.
	reconnect := transcoding.Format.Protocol == "progressive"
	cmd := ffmpegparser.NewPCMCommand(streamURL, seekSec, reconnect, "scnative-link")
	r, cleanup, err := ffmpegparser.OpusReader(cmd, "scnative")
	if err != nil {
		return parsers.Opened{}, err
	}
	return parsers.Opened{
		Reader:   r,
		Cleanup:  cleanup,
		Title:    t.Title,
		Duration: time.Duration(t.DurationMS) * time.Millisecond,
	}, nil
}
