package ytdlp

import (
	"errors"

	"github.com/keshon/melodix/pkg/music/opus"
	"github.com/keshon/melodix/pkg/music/parsers"
)

// ErrLiveStream means the URL is a live broadcast, which the link mode cannot
// serve — see the note at the check in ytdlpLink. The pipe mode handles it.
var ErrLiveStream = errors.New("ytdlp: live stream needs the pipe parser")

// YtdlpPath is the yt-dlp binary invoked by this parser; override for non-PATH installs.
var YtdlpPath = "yt-dlp"

// audioFormatSelector asks for an audio-only format and settles for a muxed one.
//
// The fallback is what makes live streams work at all. A YouTube live broadcast
// is served as HLS with no audio-only rendition — every format carries video and
// audio together — so a bare "bestaudio" is answered with "Requested format is
// not available" and the parser fails. Everything after the first "/" applies
// only in that case: wherever an audio-only format exists it still wins, so
// ordinary videos are unaffected.
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

// Streamer extracts audio by shelling out to yt-dlp; the fallback of last resort.
type Streamer struct{ Mode Mode }

func (s *Streamer) Open(track *parsers.Track, seekSec float64) (opus.Reader, func(), error) {
	if s.Mode == ModePipe {
		return ytdlpPipe(track, seekSec)
	}
	return ytdlpLink(track, seekSec)
}
