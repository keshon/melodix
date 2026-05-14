package kkdai

import (
	"bufio"
	"io"
	"strings"
)

// startFFmpegStderrReader logs ffmpeg stderr; noisy lines stay at Debug, likely-failure lines at Warn.
func startFFmpegStderrReader(parser string, stderr io.Reader) {
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			line := sc.Text()
			low := strings.ToLower(line)
			important := strings.Contains(low, "403") ||
				strings.Contains(low, "forbidden") ||
				strings.Contains(low, "server returned 4") ||
				strings.Contains(low, "unable to") ||
				strings.Contains(low, "error while") ||
				strings.Contains(low, "conversion failed")
			if important {
				log.Warn().Str("component", "kkdai").Str("parser", parser).Str("raw", line).Msg("ffmpeg_stderr")
			} else {
				log.Debug().Str("component", "kkdai").Str("parser", parser).Str("raw", line).Msg("ffmpeg_stderr")
			}
		}
	}()
}
