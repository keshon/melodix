// Package stream opens tracks as Opus packet streams and adds recovery.
package stream

import (
	"fmt"

	"github.com/keshon/melodix/pkg/music/opus"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/parsers/ffmpeg"
	"github.com/keshon/melodix/pkg/music/parsers/kkdai"
	"github.com/keshon/melodix/pkg/music/parsers/scnative"
	"github.com/keshon/melodix/pkg/music/parsers/ytdlp"
	"github.com/keshon/melodix/pkg/music/parsers/ytnative"
	"github.com/keshon/melodix/pkg/music/sources"
)

// Registry maps parser keys (the sources.Parser* constants; the strings are
// persisted in playback history, so keys are frozen identifiers) to streamers.
// The kkdai/ytdlp link-vs-pipe modes are carried on the streamer instance.
var Registry = map[string]parsers.Streamer{
	sources.ParserYtnativeLink: &ytnative.Streamer{},
	sources.ParserScnativeLink: &scnative.Streamer{},
	sources.ParserKkdaiLink:    &kkdai.Streamer{Mode: kkdai.ModeLink},
	sources.ParserKkdaiPipe:    &kkdai.Streamer{Mode: kkdai.ModePipe},
	sources.ParserYtdlpLink:    &ytdlp.Streamer{Mode: ytdlp.ModeLink},
	sources.ParserYtdlpPipe:    &ytdlp.Streamer{Mode: ytdlp.ModePipe},
	sources.ParserFFmpegLink:   &ffmpeg.Streamer{},
}

// openWithParser opens the Opus packet stream for the given parser key.
func openWithParser(track *parsers.Track, parser string, seekSec float64) (opus.Reader, func(), error) {
	streamer, ok := Registry[parser]
	if !ok {
		return nil, nil, fmt.Errorf("stream: no streamer for parser %q", parser)
	}
	return streamer.Open(track, seekSec)
}
