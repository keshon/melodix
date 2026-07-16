package kkdai

import (
	"errors"
	"fmt"
	"strings"

	"github.com/keshon/melodix/pkg/music/opus"
	"github.com/keshon/melodix/pkg/music/parsers"

	"github.com/kkdai/youtube/v2"
)

// kkdaiPipe is the kkdai passthrough path: kkdai resolves a WebM/Opus stream
// (its signature-decipher path bypasses the bot-checks that block ytnative),
// which we demux straight to Opus packets — no ffmpeg, no transcode. If the
// video offers no WebM/Opus format, or the framing isn't forwardable, it errors
// and recovery falls through to kkdai-link (ffmpeg).
func kkdaiPipe(track *parsers.Track, seekSec float64) (opus.Reader, func(), error) {
	videoID, err := extractYouTubeID(track.URL)
	if err != nil {
		return nil, nil, err
	}

	client := &youtube.Client{}
	video, err := client.GetVideo(videoID)
	if err != nil {
		return nil, nil, fmt.Errorf("kkdai: youtube client: %w", err)
	}
	track.Duration = video.Duration
	track.Title = video.Title

	f, ok := pickOpusFormat(video.Formats.WithAudioChannels())
	if !ok {
		return nil, nil, errors.New("kkdai: no webm/opus format for passthrough")
	}
	stream, _, err := client.GetStream(video, &f)
	if err != nil {
		return nil, nil, fmt.Errorf("kkdai: get stream: %w", err)
	}

	r, err := opus.Passthrough(stream, opus.SeekPackets(seekSec))
	if err != nil {
		return nil, nil, err // Passthrough closed stream
	}
	track.Passthrough = true
	l := logger()
	l.Info().Str("video_id", videoID).Msg("kkdai_passthrough")
	return r, func() { _ = r.Close() }, nil
}

// pickOpusFormat returns the highest-bitrate WebM/Opus format (itag 249/250/251).
func pickOpusFormat(formats youtube.FormatList) (youtube.Format, bool) {
	var best youtube.Format
	found := false
	for _, f := range formats {
		if !strings.Contains(f.MimeType, "audio/webm") || !strings.Contains(f.MimeType, "opus") {
			continue
		}
		if !found || f.Bitrate > best.Bitrate {
			best, found = f, true
		}
	}
	return best, found
}
