package kkdai

import (
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/keshon/melodix/pkg/music/parsers"
	ffmpegparser "github.com/keshon/melodix/pkg/music/parsers/ffmpeg"

	"github.com/kkdai/youtube/v2"
)

func kkdaiLink(track *parsers.Track, seekSec float64) (io.ReadCloser, func(), error) {
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
		return nil, nil, fmt.Errorf("kkdai: youtube client: %w", lastErr)
	}

	track.Duration = video.Duration
	track.Title = video.Title

	formats := video.Formats.WithAudioChannels()
	if len(formats) == 0 {
		return nil, nil, errors.New("kkdai: no audio formats found")
	}

	link, err := client.GetStreamURL(video, &formats[0])
	if err != nil {
		return nil, nil, fmt.Errorf("kkdai: get stream url: %w", err)
	}

	ffmpeg := ffmpegparser.NewPCMCommand(link, seekSec, true, "kkdai-link")

	reader, err := ffmpeg.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("kkdai: stdout pipe: %w", err)
	}

	if err := ffmpeg.Start(); err != nil {
		return nil, nil, fmt.Errorf("kkdai: ffmpeg start: %w", err)
	}

	pr := ffmpegparser.NewProcessStream(ffmpeg, reader)
	cleanup := func() {
		_ = pr.Close()
	}

	return pr, cleanup, nil
}
