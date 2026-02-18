package parsers

import (
	"time"

	"github.com/keshon/melodix/internal/music/sources"
)

type TrackParse struct {
	URL                 string
	Title               string
	Artist              string
	Duration            time.Duration
	CurrentPlayDuration time.Duration
	CurrentParser       string
	SourceInfo          sources.TrackInfo
}
