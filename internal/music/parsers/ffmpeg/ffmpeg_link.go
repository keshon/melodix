package ffmpeg

import (
	"fmt"
	"io"
	"os/exec"
)

func ffmpegLink(url string) (io.ReadCloser, func(), error) {
	cmd := exec.Command("ffmpeg",
		"-reconnect", "1",
		"-reconnect_streamed", "1",
		"-reconnect_delay_max", "5",
		"-i", url,
		"-f", "s16le",
		"-ar", fmt.Sprintf("%d", sampleRate),
		"-ac", fmt.Sprintf("%d", channels),
		"-loglevel", "warning",
		"pipe:1",
	)

	reader, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("stdout pipe error: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("command start error: %w", err)
	}

	cleanup := func() {
		cmd.Process.Kill()
	}

	return reader, cleanup, nil
}
