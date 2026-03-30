package musicapp

import (
	"fmt"

	"github.com/keshon/melodix/internal/history"
	"github.com/keshon/melodix/internal/playinput"
	"github.com/keshon/melodix/internal/storage"
	"github.com/keshon/melodix/pkg/music/player"
)

// Facade is the narrow interface for music application operations. Implementations: [*Music].
type Facade interface {
	BuildHistoryPage(guildID string, page int64, view string) (*HistoryPage, error)
	EnqueueFromParsedInput(p *player.Player, guildID string, parsed playinput.ParsedPlayInput, source, parser string, resolve ResolveFunc, queryMode QueryEnqueueMode) error
}

// Music is the concrete facade: inject [*storage.Storage] and call from adapters.
type Music struct {
	Store *storage.Storage
}

// New returns a [Music] facade for the given store (may be nil; methods will error when storage is required).
func New(store *storage.Storage) *Music {
	return &Music{Store: store}
}

// BuildHistoryPage returns one page of playback history for the guild.
func (m *Music) BuildHistoryPage(guildID string, page int64, view string) (*HistoryPage, error) {
	if m == nil || m.Store == nil {
		return nil, fmt.Errorf("musicapp: storage is nil")
	}
	return history.BuildHistoryPage(m.Store, guildID, page, view)
}

var _ Facade = (*Music)(nil)
