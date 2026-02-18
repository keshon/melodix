package kkdai

import (
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"

	"github.com/keshon/melodix/internal/music/parsers"

	"github.com/kkdai/youtube/v2"
)

func kkdaiLink(track *parsers.TrackParse, seekSec float64) (io.ReadCloser, func(), error) {
	videoID, err := extractYouTubeID(track.URL)
	if err != nil {
		return nil, nil, err
	}

	type res struct {
		client *youtube.Client
		video  *youtube.Video
		err    error
	}

	ch := make(chan res, 1)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		client := &youtube.Client{}
		video, err := client.GetVideo(videoID)
		ch <- res{client: client, video: video, err: err}
	}()

	go func() {
		wg.Wait()
		close(ch)
	}()

	var client *youtube.Client
	var video *youtube.Video
	var lastErr error

	for r := range ch {
		if r.err == nil {
			client = r.client
			video = r.video
			break
		} else {
			lastErr = r.err
		}
	}

	if client == nil || video == nil {
		return nil, nil, fmt.Errorf("[kkdai-link] youtube client error: %w", lastErr)
	}

	track.Duration = video.Duration
	track.Title = video.Title

	formats := video.Formats.WithAudioChannels()
	if len(formats) == 0 {
		return nil, nil, errors.New("[kkdai-link] no audio formats found for video")
	}

	link, err := client.GetStreamURL(video, &formats[0])
	if err != nil {
		return nil, nil, fmt.Errorf("[kkdai-link] get stream URL error: %w", err)
	}

	ffmpeg := exec.Command("ffmpeg",
		"-ss", fmt.Sprintf("%.3f", seekSec),
		"-reconnect", "1",
		"-reconnect_streamed", "1",
		"-reconnect_delay_max", "5",
		"-i", link,
		"-f", "s16le",
		"-ar", fmt.Sprintf("%d", sampleRate),
		"-ac", fmt.Sprintf("%d", channels),
		"-loglevel", "warning",
		"pipe:1",
	)

	reader, err := ffmpeg.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("stdout pipe error: %w", err)
	}

	if err := ffmpeg.Start(); err != nil {
		return nil, nil, fmt.Errorf("command start error: %w", err)
	}

	cleanup := func() {
		ffmpeg.Process.Kill()
	}

	return reader, cleanup, nil
}
