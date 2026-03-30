package command

import (
	"io"

	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/musicapp"
	"github.com/keshon/melodix/internal/storage"
	"github.com/keshon/melodix/pkg/music/player"
	"github.com/keshon/melodix/pkg/music/resolver"
)

// Data is the per-invocation payload for CLI commandkit commands (no Discord/CLI cycle).
type Data struct {
	Config     *config.Config
	Store      *storage.Storage
	Player     *player.Player
	Resolver   *resolver.SourceResolver
	GuildScope string
	Out        io.Writer
	// Music is the application facade for history and enqueue (same store as Store).
	Music *musicapp.Music
}
