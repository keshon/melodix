package ytdlp

import (
	"github.com/keshon/melodix/pkg/music/opus"
	"github.com/keshon/melodix/pkg/music/parsers"
)

// YtdlpPath is the yt-dlp binary invoked by this parser; override for non-PATH installs.
var YtdlpPath = "yt-dlp"

// audioFormatSelector asks for an audio-only format and settles for a muxed one.
//
// The fallback is what makes live streams work at all. A YouTube live broadcast
// is served as HLS with no audio-only rendition — every format carries video and
// audio together — so a bare "bestaudio" is answered with "Requested format is
// not available" and the parser fails. With "/best" yt-dlp hands back a muxed
// HLS stream instead and ffmpeg drops the video, which costs a little bandwidth
// on live streams and nothing at all elsewhere, since an audio-only format is
// still preferred whenever one exists.
const audioFormatSelector = "bestaudio/best"

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
