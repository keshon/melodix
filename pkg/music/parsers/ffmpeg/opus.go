package ffmpeg

import (
	"fmt"
	"os/exec"

	"github.com/keshon/melodix/pkg/music/opus"
)

// OpusReader starts cmd (ffmpeg emitting PCM s16le on stdout), wraps it as a
// ProcessStream, and returns an opus.Reader that encodes each 20ms frame. This
// is the shared tail of every ffmpeg-backed streamer; tag prefixes errors.
// The returned cleanup kills ffmpeg and closes the stream.
func OpusReader(cmd *exec.Cmd, tag string) (opus.Reader, func(), error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("%s: stdout pipe: %w", tag, err)
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("%s: ffmpeg start: %w", tag, err)
	}
	r := opus.Encode(NewProcessStream(cmd, stdout))
	return r, func() { _ = r.Close() }, nil
}
