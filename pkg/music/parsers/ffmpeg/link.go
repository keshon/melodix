package ffmpeg

import (
	"fmt"
	"io"
)

func ffmpegLink(url string) (io.ReadCloser, func(), error) {
	cmd := NewPCMCommand(url, 0, true, "ffmpeg-link")

	reader, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("stdout pipe error: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("command start error: %w", err)
	}

	pr := NewProcessStream(cmd, reader)
	cleanup := func() {
		_ = pr.Close()
	}

	return pr, cleanup, nil
}
