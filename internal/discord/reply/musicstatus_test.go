package reply

import (
	"testing"
	"time"

	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/sources"
)

func track(source, parser, artist string, d time.Duration) *parsers.Track {
	return &parsers.Track{
		URL:           "https://example.com/t",
		Title:         "Song",
		Artist:        artist,
		Duration:      d,
		CurrentParser: parser,
		SourceInfo:    sources.TrackInfo{SourceName: source},
	}
}

func passthroughTrack(source, parser string, d time.Duration) *parsers.Track {
	tr := track(source, parser, "", d)
	tr.Passthrough = true
	return tr
}

func cachedTrack(source string, d time.Duration) *parsers.Track {
	tr := track(source, "", "", d) // cache sets no CurrentParser
	tr.Cached = true
	return tr
}

func TestNowPlayingEmbedChips(t *testing.T) {
	cases := []struct {
		name  string
		track *parsers.Track
		want  string // full expected description
	}{
		{
			name:  "passthrough track",
			track: passthroughTrack("youtube", "kkdai-pipe", 212*time.Second),
			want:  "🎶 [Song](https://example.com/t)\n\n`youtube` `kkdai-pipe` `passthrough` `3:32`",
		},
		{
			name:  "cached track shows cached chip, no parser/method",
			track: cachedTrack("youtube", 212*time.Second),
			want:  "🎶 [Song](https://example.com/t)\n\n`youtube` `cached` `3:32`",
		},
		{
			name:  "ffmpeg track with artist",
			track: track("youtube", "ytnative-link", "Rick Astley", 212*time.Second),
			want:  "🎶 [Song](https://example.com/t)\n\n`youtube` `ytnative-link` `ffmpeg` `3:32` `Rick Astley`",
		},
		{
			name:  "radio shows live instead of duration",
			track: track(sources.Radio, "ffmpeg-link", "", 0),
			want:  "🎶 [Song](https://example.com/t)\n\n`radio` `ffmpeg-link` `ffmpeg` `live`",
		},
		{
			name:  "unknown duration omitted",
			track: track("soundcloud", "scnative-link", "", 0),
			want:  "🎶 [Song](https://example.com/t)\n\n`soundcloud` `scnative-link` `ffmpeg`",
		},
		{
			name:  "hour-long formatting",
			track: passthroughTrack("youtube", "kkdai-pipe", time.Hour+5*time.Minute+7*time.Second),
			want:  "🎶 [Song](https://example.com/t)\n\n`youtube` `kkdai-pipe` `passthrough` `1:05:07`",
		},
		{
			name:  "no metadata means no chip line",
			track: &parsers.Track{Title: "Song", URL: "https://example.com/t"},
			want:  "🎶 [Song](https://example.com/t)",
		},
		{
			name:  "nil track falls back",
			track: nil,
			want:  "🎶 Unknown track",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NowPlayingEmbed(tc.track).Description
			if got != tc.want {
				t.Fatalf("description:\n got %q\nwant %q", got, tc.want)
			}
		})
	}
}
