package playhistory

import (
	"log"
	"time"

	"github.com/keshon/melodix/internal/storage"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/player"
)

type recorder struct {
	store *storage.Storage
}

func (r recorder) Record(guildID string, playedAt time.Time, track parsers.TrackParse) {
	if r.store == nil {
		return
	}
	if _, err := r.store.AppendMusicPlayback(guildID, track, playedAt); err != nil {
		log.Printf("[music] append playback history: %v", err)
	}
}

// NewRecorder returns a player.PlaybackRecorder that persists successful track starts per guild.
func NewRecorder(store *storage.Storage) player.PlaybackRecorder {
	return recorder{store: store}
}
