package ytdlp

import (
	"github.com/keshon/melodix/pkg/music/opus"
	"github.com/keshon/melodix/pkg/music/parsers"
)

// YtdlpPath is the yt-dlp binary invoked by this parser; override for non-PATH installs.
var YtdlpPath = "yt-dlp"

// Mode selects how yt-dlp feeds ffmpeg: Link resolves a CDN URL for ffmpeg;
// Pipe streams yt-dlp's stdout into ffmpeg's stdin.
type Mode int

const (
	ModeLink Mode = iota
	ModePipe
)

// Streamer extracts audio by shelling out to yt-dlp; the fallback of last resort.
type Streamer struct{ Mode Mode }

func (s *Streamer) Open(track *parsers.Track, seekSec float64) (opus.Reader, func(), error) {
	if s.Mode == ModePipe {
		return ytdlpPipe(track, seekSec)
	}
	return ytdlpLink(track, seekSec)
}
