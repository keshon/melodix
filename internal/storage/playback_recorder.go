package storage

import (
	"log"
	"time"

	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/player"
)

type playbackRecorder struct {
	store *Storage
}

func (r playbackRecorder) Record(guildID string, playedAt time.Time, track parsers.TrackParse) {
	if r.store == nil {
		return
	}
	if _, err := r.store.AppendMusicPlayback(guildID, track, playedAt); err != nil {
		log.Printf("[music] append playback history: %v", err)
	}
}

// NewPlaybackRecorder returns a player.PlaybackRecorder that persists successful track starts per guild.
func (s *Storage) NewPlaybackRecorder() player.PlaybackRecorder {
	return playbackRecorder{store: s}
}
