package ytnative

import (
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/keshon/melodix/pkg/music/parsers"
	ffmpegparser "github.com/keshon/melodix/pkg/music/parsers/ffmpeg"
)

func ytnativeLink(track *parsers.TrackParse, seekSec float64) (io.ReadCloser, func(), error) {
	videoID, err := extractVideoID(track.URL)
	if err != nil {
		return nil, nil, err
	}

	pr, err := fetchPlayer(httpClient, playerEndpoint, videoID)
	if err != nil {
		l := logger()
		l.Warn().Str("video_id", videoID).Str("client_version", clientVersion).Err(err).Msg("ytnative_player_failed")
		return nil, nil, err
	}

	if pr.VideoDetails.Title != "" {
		track.Title = pr.VideoDetails.Title
	}
	if secs, err := strconv.Atoi(pr.VideoDetails.LengthSeconds); err == nil && secs > 0 {
		track.Duration = time.Duration(secs) * time.Second
	}

	f, err := pickAudioFormat(pr.StreamingData.AdaptiveFormats)
	if err != nil {
		return nil, nil, err
	}

	// The CDN must see the same client that requested the URL.
	cmd := ffmpegparser.NewPCMCommandUA(f.URL, seekSec, true, "ytnative-link", clientUserAgent)

	reader, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("stdout pipe error: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("command start error: %w", err)
	}

	pr2 := ffmpegparser.NewProcessStream(cmd, reader)
	cleanup := func() {
		_ = pr2.Close()
	}
	return pr2, cleanup, nil
}
