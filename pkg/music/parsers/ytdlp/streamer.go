package ytdlp

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync/atomic"

	"github.com/keshon/melodix/pkg/music/opus"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/rs/zerolog"
)

var logPtr atomic.Pointer[zerolog.Logger]

// SetLogger sets the package logger (which YouTube client and JS runtime this
// parser settled on). Safe for concurrent use; call once at process startup.
func SetLogger(l zerolog.Logger) {
	logPtr.Store(&l)
}

func logger() zerolog.Logger {
	if l := logPtr.Load(); l != nil {
		return *l
	}
	return zerolog.Nop()
}

// runJSON runs a yt-dlp command and returns its stdout, folding stderr into the
// error when it fails.
//
// Without this, a failure reaches the log as "exit status 1" and yt-dlp's own
// explanation is discarded — which is exactly how a misconfigured environment
// stayed invisible: the real message was "No supported JavaScript runtime could
// be found ... some formats may be missing", and nothing carried it up.
func runJSON(cmd *exec.Cmd, what string) ([]byte, error) {
	out, err := cmd.Output()
	if err == nil {
		return out, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && len(ee.Stderr) > 0 {
		return nil, fmt.Errorf("ytdlp: %s: %w: %s", what, err, lastLines(string(ee.Stderr), 2))
	}
	return nil, fmt.Errorf("ytdlp: %s: %w", what, err)
}

// lastLines keeps the tail of yt-dlp's stderr: the actionable message is the
// final one, while everything before it is progress noise.
func lastLines(s string, n int) string {
	lines := make([]string, 0, n)
	for _, l := range strings.Split(strings.TrimSpace(s), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}

// YtdlpPath is the yt-dlp binary invoked by this parser; override for non-PATH
// installs.
var YtdlpPath = "yt-dlp"

// audioFormatSelector asks for an audio-only format and settles for a muxed
// one.
//
// The fallback is what makes live streams work at all. A YouTube live broadcast
// is served as HLS with no audio-only rendition — every format carries video
// and audio together — so a bare "bestaudio" is answered with "Requested format
// is not available" and the parser fails. Everything after the first "/"
// applies only in that case: wherever an audio-only format exists it still
// wins, so ordinary videos are unaffected.
//
// The 360p ceiling on the muxed fallback is about bandwidth we would otherwise
// throw away. Measured on one live broadcast, the renditions ran 269, 507, 962,
// 1282, 2922 and 5552 kbit/s — and "best" means the last of those, five and a
// half megabits per second fetched to extract audio and discard the picture.
// 360p is the cheapest rendition that still carries AAC-LC: below it YouTube
// switches to HE-AAC, so going lower starts costing audio quality rather than
// just picture. "worst" is the final fallback for a stream that offers nothing
// at or under 360p.
const audioFormatSelector = "bestaudio/best[height<=360]/worst"

// Mode selects how yt-dlp feeds ffmpeg: Link resolves a CDN URL for ffmpeg;
// Pipe streams yt-dlp's stdout into ffmpeg's stdin.
type Mode int

const (
	ModeLink Mode = iota
	ModePipe
)

// Streamer extracts audio by shelling out to yt-dlp; the fallback of last
// resort.
type Streamer struct{ Mode Mode }

func (s *Streamer) Open(track *parsers.Track, seekSec float64) (opus.Reader, func(), error) {
	if s.Mode == ModePipe {
		return ytdlpPipe(track, seekSec)
	}
	return ytdlpLink(track, seekSec)
}
