package kkdai

import (
	"errors"
	"fmt"
	"io"

	"github.com/keshon/melodix/pkg/music/parsers"
	ffmpegparser "github.com/keshon/melodix/pkg/music/parsers/ffmpeg"

	"github.com/kkdai/youtube/v2"
)

func kkdaiPipe(track *parsers.Track, seekSec float64) (io.ReadCloser, func(), error) {
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

	formats := video.Formats.WithAudioChannels()
	if len(formats) == 0 {
		return nil, nil, errors.New("kkdai: no audio formats found")
	}

	stream, _, err := client.GetStream(video, &formats[0])
	if err != nil {
		return nil, nil, fmt.Errorf("kkdai: get stream: %w", err)
	}

	l := logger()
	l.Debug().Msg("stream_size_unknown_piping")

	ffmpeg := ffmpegparser.NewPCMCommand("pipe:0", seekSec, false, "kkdai-pipe")

	ffmpeg.Stdin = stream
	reader, err := ffmpeg.StdoutPipe()
	if err != nil {
		_ = stream.Close()
		return nil, nil, fmt.Errorf("kkdai: ffmpeg stdout pipe: %w", err)
	}

	if err := ffmpeg.Start(); err != nil {
		_ = stream.Close()
		return nil, nil, fmt.Errorf("kkdai: ffmpeg start: %w", err)
	}

	pr := ffmpegparser.NewProcessStream(ffmpeg, reader)
	cleanup := func() {
		stream.Close()
		_ = pr.Close()
	}

	return pr, cleanup, nil
}
