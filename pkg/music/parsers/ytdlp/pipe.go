package ytdlp

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/keshon/melodix/pkg/music/parsers"
	ffmpegparser "github.com/keshon/melodix/pkg/music/parsers/ffmpeg"
)

func ytdlpPipe(track parsers.Track, seekSec float64) (parsers.Opened, error) {
	output, err := runJSON(exec.Command(YtdlpPath, args("-j", "-f", audioFormatSelector, track.URL)...), "get json")
	if err != nil {
		return parsers.Opened{}, err
	}

	type fragment struct {
		Duration float64 `json:"duration"`
	}

	type format struct {
		Fragments []fragment `json:"fragments,omitempty"`
	}

	type ytdlpInfo struct {
		Duration float64  `json:"duration"`
		Formats  []format `json:"formats"`
	}

	var info ytdlpInfo
	if err := json.Unmarshal(output, &info); err != nil {
		return parsers.Opened{}, fmt.Errorf("ytdlp: decode json: %w", err)
	}

	if info.Duration == 0 && len(info.Formats) > 0 {
		if len(info.Formats[0].Fragments) > 0 {
			info.Duration = info.Formats[0].Fragments[0].Duration
		}
	}

	ytdlp := exec.Command(YtdlpPath, args("-o", "-", "-f", audioFormatSelector, track.URL)...)
	ffmpeg := ffmpegparser.NewPCMCommand("pipe:0", seekSec, false, "ytdlp-pipe")

	ffmpegIn, err := ytdlp.StdoutPipe()
	if err != nil {
		return parsers.Opened{}, fmt.Errorf("ytdlp: yt-dlp stdout pipe: %w", err)
	}
	ffmpeg.Stdin = ffmpegIn

	if err := ytdlp.Start(); err != nil {
		return parsers.Opened{}, fmt.Errorf("ytdlp: yt-dlp start: %w", err)
	}

	r, cleanup, err := ffmpegparser.OpusReader(ffmpeg, "ytdlp")
	if err != nil {
		_ = ytdlp.Process.Kill()
		return parsers.Opened{}, err
	}
	return parsers.Opened{
		Reader: r,
		Cleanup: func() {
			cleanup()
			_ = ytdlp.Process.Kill()
			_ = ytdlp.Wait()
		},
		Duration: time.Duration(info.Duration * float64(time.Second)),
	}, nil
}
