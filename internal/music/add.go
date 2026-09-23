package music

import (
	"errors"
	"fmt"

	"github.com/keshon/melodix/internal/storage"
	"github.com/keshon/melodix/pkg/music/sources"
)

// The ways Add fails, for a frontend to word. Each is wrapped around its cause
// where there is one.
var (
	// ErrHistoryUnavailable means the input named history ids and there is no
	// storage: the CLI while the bot holds the data directory, say.
	ErrHistoryUnavailable = errors.New("playback history is not available")
	// ErrUnknownHistoryID means an id is not in the scope's history, most
	// likely trimmed away.
	ErrUnknownHistoryID = errors.New("unknown history id")
	// ErrHistoryLoad means the history could not be read.
	ErrHistoryLoad = errors.New("could not load history")
	// ErrResolve means an input resolved to nothing playable.
	ErrResolve = errors.New("failed to resolve track")
	// ErrQueue means the tracks resolved but the player refused them.
	ErrQueue = errors.New("failed to queue")
)

// errNothingFound is the cause when a resolve succeeds with no tracks.
var errNothingFound = errors.New("nothing found")

// Add queues what in names on the scope's player and returns how many tracks
// it queued. It never starts playback -- see Service.
//
// Everything is resolved before anything is queued: a request is queued whole
// or not at all, and a batch emits a single queue update.
func (s *Service) Add(scope string, in Input, source, parser string) (int, error) {
	var batch []sources.TrackInfo
	switch in.Kind {
	case InputHistoryIDs:
		if s.store == nil {
			return 0, ErrHistoryUnavailable
		}
		batch = make([]sources.TrackInfo, 0, len(in.HistoryIDs))
		for _, id := range in.HistoryIDs {
			entry, err := s.store.MusicPlayback(scope, id)
			if errors.Is(err, storage.ErrMusicPlaybackNotFound) {
				return 0, fmt.Errorf("%w: %d", ErrUnknownHistoryID, id)
			}
			if err != nil {
				return 0, fmt.Errorf("%w: %w", ErrHistoryLoad, err)
			}
			batch = append(batch, storage.TrackInfoFromMusicPlayback(entry))
		}

	case InputURLs:
		batch = make([]sources.TrackInfo, 0, len(in.URLs))
		for _, u := range in.URLs {
			tracks, err := s.resolve(u, source, parser)
			if err != nil {
				return 0, err
			}
			batch = append(batch, tracks...)
		}

	case InputQuery:
		tracks, err := s.resolve(in.Query, source, parser)
		if err != nil {
			return 0, err
		}
		batch = tracks
	}

	if err := s.Player(scope).EnqueueTrackInfos(batch); err != nil {
		return 0, fmt.Errorf("%w: %w", ErrQueue, err)
	}
	return len(batch), nil
}

func (s *Service) resolve(input, source, parser string) ([]sources.TrackInfo, error) {
	tracks, err := s.resolver.Resolve(input, source, parser)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrResolve, err)
	}
	if len(tracks) == 0 {
		return nil, fmt.Errorf("%w: %w", ErrResolve, errNothingFound)
	}
	return tracks, nil
}
