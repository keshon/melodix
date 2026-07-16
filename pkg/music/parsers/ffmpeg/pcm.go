package ffmpeg

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"sync/atomic"

	"github.com/rs/zerolog"
)

// FFmpegPath is the ffmpeg binary invoked by all parsers; override for non-PATH installs.
var FFmpegPath = "ffmpeg"

var logPtr atomic.Pointer[zerolog.Logger]

// SetLogger sets the package logger used for ffmpeg stderr classification.
// Safe for concurrent use; call once at process startup.
func SetLogger(l zerolog.Logger) {
	logPtr.Store(&l)
}

func logger() zerolog.Logger {
	if l := logPtr.Load(); l != nil {
		return *l
	}
	return zerolog.Nop()
}

// NewPCMCommand builds the ffmpeg invocation shared by all parsers: decode input
// (a URL or "pipe:0") to raw PCM s16le 48kHz stereo on stdout. reconnect adds HTTP
// reconnect flags (URL inputs only). Stderr is classified and logged line-by-line
// under tag via the package logger; exec.Cmd owns the stderr copy and Wait
// synchronizes with it, so there is no StderrPipe-vs-Wait race.
func NewPCMCommand(input string, seekSec float64, reconnect bool, tag string) *exec.Cmd {
	return NewPCMCommandUA(input, seekSec, reconnect, tag, "")
}

// NewPCMCommandUA is NewPCMCommand with an HTTP User-Agent for URL inputs
// ("" keeps ffmpeg's default). Some CDNs (e.g. googlevideo) reject requests
// whose UA does not match the client that obtained the stream URL.
func NewPCMCommandUA(input string, seekSec float64, reconnect bool, tag, userAgent string) *exec.Cmd {
	args := make([]string, 0, 18)
	if seekSec > 0 {
		args = append(args, "-ss", fmt.Sprintf("%.3f", seekSec))
	}
	if reconnect {
		args = append(args, "-reconnect", "1", "-reconnect_streamed", "1", "-reconnect_delay_max", "5")
	}
	if userAgent != "" {
		args = append(args, "-user_agent", userAgent)
	}
	args = append(args,
		"-i", input,
		"-f", "s16le",
		"-ar", fmt.Sprintf("%d", sampleRate),
		"-ac", fmt.Sprintf("%d", channels),
		"-loglevel", "warning",
		"pipe:1",
	)
	cmd := exec.Command(FFmpegPath, args...)
	cmd.Stderr = &stderrLineWriter{tag: tag}
	return cmd
}

// stderrLineWriter splits ffmpeg stderr into lines: likely-failure lines log at Warn,
// the rest at Debug to limit noise.
type stderrLineWriter struct {
	tag string
	buf []byte
}

func (w *stderrLineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			return len(p), nil
		}
		w.logLine(strings.TrimRight(string(w.buf[:i]), "\r"))
		w.buf = w.buf[i+1:]
	}
}

func (w *stderrLineWriter) logLine(line string) {
	if line == "" {
		return
	}
	low := strings.ToLower(line)
	important := strings.Contains(low, "403") ||
		strings.Contains(low, "forbidden") ||
		strings.Contains(low, "server returned 4") ||
		strings.Contains(low, "unable to") ||
		strings.Contains(low, "error while") ||
		strings.Contains(low, "conversion failed")
	l := logger()
	evt := l.Debug()
	if important {
		evt = l.Warn()
	}
	evt.Str("component", "ffmpeg").Str("parser", w.tag).Str("raw", line).Msg("ffmpeg_stderr")
}
