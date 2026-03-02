package kkdai

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os/exec"

	"github.com/keshon/melodix/pkg/music/parsers"

	"github.com/kkdai/youtube/v2"
)

func kkdaiPipe(track *parsers.TrackParse, seekSec float64) (io.ReadCloser, func(), error) {
	videoID, err := extractYouTubeID(track.URL)
	if err != nil {
		return nil, nil, err
	}

	client := &youtube.Client{}
	video, err := client.GetVideo(videoID)
	if err != nil {
		return nil, nil, fmt.Errorf("[kkdai-pipe] youtube client error: %w", err)
	}

	track.Duration = video.Duration
	track.Title = video.Title

	formats := video.Formats.WithAudioChannels()
	if len(formats) == 0 {
		return nil, nil, errors.New("[kkdai-pipe] no audio formats found for video")
	}

	stream, _, err := client.GetStream(video, &formats[0])
	if err != nil {
		return nil, nil, fmt.Errorf("get stream error: %w", err)
	}

	log.Printf("[kkdai-pipe] stream size: unknown (piping)\n") // size not used

	ffmpeg := exec.Command("ffmpeg",
		"-ss", fmt.Sprintf("%.3f", seekSec),
		"-i", "pipe:0",
		"-f", "s16le",
		"-ar", fmt.Sprintf("%d", sampleRate),
		"-ac", fmt.Sprintf("%d", channels),
		"-loglevel", "warning",
		"pipe:1",
	)

	ffmpeg.Stdin = stream
	reader, err := ffmpeg.StdoutPipe()
	if err != nil {
		stream.Close()
		return nil, nil, fmt.Errorf("ffmpeg stdout pipe error: %w", err)
	}

	if err := ffmpeg.Start(); err != nil {
		stream.Close()
		return nil, nil, fmt.Errorf("ffmpeg start error: %w", err)
	}

	cleanup := func() {
		stream.Close()
		ffmpeg.Process.Kill()
	}

	return reader, cleanup, nil
}
