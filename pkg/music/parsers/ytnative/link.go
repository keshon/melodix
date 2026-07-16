package ytnative

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/keshon/melodix/pkg/music/opus"
	"github.com/keshon/melodix/pkg/music/parsers"
	ffmpegparser "github.com/keshon/melodix/pkg/music/parsers/ffmpeg"
)

func ytnativeLink(track *parsers.Track, seekSec float64) (opus.Reader, func(), error) {
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

	// Passthrough: forward YouTube's WebM/Opus straight to Discord — no ffmpeg,
	// no transcode. Falls back to the ffmpeg-encode path if unavailable or the
	// stream's framing isn't what the Discord sender can forward.
	if f, ok := pickOpusFormat(pr.StreamingData.AdaptiveFormats); ok {
		r, cleanup, err := openPassthrough(f.URL, seekSec)
		l := logger()
		if err == nil {
			track.Passthrough = true
			l.Info().Str("video_id", videoID).Int("bitrate", f.Bitrate).Msg("ytnative_passthrough")
			return r, cleanup, nil
		}
		l.Warn().Str("video_id", videoID).Err(err).Msg("ytnative_passthrough_failed_ffmpeg_fallback")
	}

	// Fallback: ffmpeg-encode the best available audio format.
	f, err := pickAudioFormat(pr.StreamingData.AdaptiveFormats)
	if err != nil {
		return nil, nil, err
	}
	cmd := ffmpegparser.NewPCMCommandUA(f.URL, seekSec, true, "ytnative-link", clientUserAgent)
	return ffmpegparser.OpusReader(cmd, "ytnative")
}

// openPassthrough streams the WebM/Opus URL and demuxes it to Opus packets
// (no ffmpeg). Seek re-fetches from the start and discards to the position
// (v2: HTTP Range). Framing is validated by opus.Passthrough.
func openPassthrough(url string, seekSec float64) (opus.Reader, func(), error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", clientUserAgent)
	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, nil, fmt.Errorf("ytnative: cdn %s", resp.Status)
	}
	r, err := opus.Passthrough(resp.Body, opus.SeekPackets(seekSec))
	if err != nil {
		return nil, nil, err // Passthrough closed resp.Body
	}
	return r, func() { _ = r.Close() }, nil
}
