package ytdlp

import (
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"time"

	"github.com/keshon/melodix/pkg/music/parsers"
	ffmpegparser "github.com/keshon/melodix/pkg/music/parsers/ffmpeg"
)

func ytdlpPipe(track *parsers.TrackParse, seekSec float64) (io.ReadCloser, func(), error) {
	ytdlp := exec.Command(YtdlpPath, "-j", "-f", "bestaudio", track.URL)
	output, err := ytdlp.Output()
	if err != nil {
		return nil, nil, fmt.Errorf("yt-dlp json error: %w", err)
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
		return nil, nil, fmt.Errorf("json unmarshal error: %w", err)
	}

	if info.Duration == 0 && len(info.Formats) > 0 {
		if len(info.Formats[0].Fragments) > 0 {
			info.Duration = info.Formats[0].Fragments[0].Duration
		}
	}

	track.Duration = time.Duration(info.Duration * float64(time.Second))

	ytdlp = exec.Command(YtdlpPath, "-o", "-", "-f", "bestaudio", track.URL)
	ffmpeg := ffmpegparser.NewPCMCommand("pipe:0", seekSec, false, "ytdlp-pipe")

	ffmpegIn, err := ytdlp.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("yt-dlp stdout pipe error: %w", err)
	}
	ffmpeg.Stdin = ffmpegIn

	reader, err := ffmpeg.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("ffmpeg stdout pipe error: %w", err)
	}

	if err := ytdlp.Start(); err != nil {
		return nil, nil, fmt.Errorf("yt-dlp start error: %w", err)
	}
	if err := ffmpeg.Start(); err != nil {
		ytdlp.Process.Kill()
		return nil, nil, fmt.Errorf("ffmpeg start error: %w", err)
	}

	pr := ffmpegparser.NewProcessStream(ffmpeg, reader)
	cleanup := func() {
		_ = pr.Close()
		_ = ytdlp.Process.Kill()
		_ = ytdlp.Wait()
	}

	return pr, cleanup, nil
}
