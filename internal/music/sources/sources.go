package sources

const (
	SourceAuto       = "auto"
	SourceYouTube    = "youtube"
	SourceRadio      = "radio"
	SourceSoundCloud = "soundcloud"
)

type TrackInfo struct {
	URL              string
	Title            string
	SourceName       string
	AvailableParsers []string
}
