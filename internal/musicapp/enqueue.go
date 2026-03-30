package musicapp

import (
	"errors"
	"fmt"

	"github.com/keshon/melodix/internal/playinput"
	"github.com/keshon/melodix/internal/storage"
	"github.com/keshon/melodix/pkg/music/player"
	"github.com/keshon/melodix/pkg/music/sources"
)

var (
	// ErrStorageUnavailable is returned when history playback needs storage but it is nil.
	ErrStorageUnavailable = errors.New("music history storage is not available")
	// ErrNoTracksResolved is returned when resolve yields no tracks for a URL or query.
	ErrNoTracksResolved = errors.New("no tracks resolved")
)

// QueryEnqueueMode selects how a single query string is turned into queue entries.
type QueryEnqueueMode int

const (
	// QueryViaPlayerEnqueue uses player.Enqueue (resolver inside player) — typical for CLI.
	QueryViaPlayerEnqueue QueryEnqueueMode = iota
	// QueryViaResolveFirst resolves externally and enqueues the first result — typical for Discord.
	QueryViaResolveFirst
)

// ResolveFunc resolves one input string to track metadata (URL or search).
type ResolveFunc func(input, source, parser string) ([]sources.TrackInfo, error)

// EnqueueFromParsedInput enqueues tracks from classified play input. resolve is required for
// PlayInputKindURLs and for PlayInputKindQuery when mode is QueryViaResolveFirst.
func (m *Music) EnqueueFromParsedInput(p *player.Player, guildID string, parsed playinput.ParsedPlayInput, source, parser string, resolve ResolveFunc, queryMode QueryEnqueueMode) error {
	store := m.Store
	switch parsed.Kind {
	case playinput.PlayInputKindHistoryIDs:
		return enqueueHistoryIDs(p, store, guildID, parsed.HistoryIDs)
	case playinput.PlayInputKindURLs:
		if resolve == nil {
			return fmt.Errorf("resolve is required for URL batch")
		}
		for _, u := range parsed.URLs {
			tracks, err := resolve(u, source, parser)
			if err != nil {
				return err
			}
			if len(tracks) == 0 {
				return fmt.Errorf("%w for %q", ErrNoTracksResolved, u)
			}
			if err := p.EnqueueTrackInfo(tracks[0]); err != nil {
				return err
			}
		}
	case playinput.PlayInputKindQuery:
		switch queryMode {
		case QueryViaPlayerEnqueue:
			return p.Enqueue(parsed.Query, source, parser)
		default:
			if resolve == nil {
				return fmt.Errorf("resolve is required for query")
			}
			tracks, err := resolve(parsed.Query, source, parser)
			if err != nil {
				return err
			}
			if len(tracks) == 0 {
				return ErrNoTracksResolved
			}
			return p.EnqueueTrackInfo(tracks[0])
		}
	}
	return nil
}

func enqueueHistoryIDs(p *player.Player, store *storage.Storage, guildScope string, ids []uint64) error {
	if store == nil {
		return ErrStorageUnavailable
	}
	for _, hid := range ids {
		mp, gerr := store.GetMusicPlayback(guildScope, hid)
		if gerr != nil {
			if errors.Is(gerr, storage.ErrMusicPlaybackNotFound) {
				return fmt.Errorf("unknown history id %d: %w", hid, storage.ErrMusicPlaybackNotFound)
			}
			return fmt.Errorf("could not load history entry: %w", gerr)
		}
		ti := storage.TrackInfoFromMusicPlayback(mp)
		if err := p.EnqueueTrackInfo(ti); err != nil {
			return err
		}
	}
	return nil
}
