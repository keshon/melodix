package music

import (
	"context"

	"github.com/keshon/melodix/pkg/music/player"
	"github.com/keshon/melodix/pkg/music/sources"
)

// VoiceState is the minimal voice channel state needed for music commands.
type VoiceState struct {
	ChannelID string
	UserID    string
}

// PlayerProvider provides access to the guild-scoped playback engine.
// Implemented by the Discord bot adapter (delegates to voice service).
type PlayerProvider interface {
	GetOrCreatePlayer(guildID string) *player.Player
}

// TrackResolver resolves user input (query or URL) into track metadata.
// Implemented by the Discord bot adapter (delegates to voice service).
type TrackResolver interface {
	Resolve(ctx context.Context, input, source, parser string) ([]sources.TrackInfo, error)
}
