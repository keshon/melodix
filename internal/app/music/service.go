package music

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/keshon/melodix/internal/domain"
	"github.com/keshon/melodix/pkg/music/player"
	"github.com/keshon/melodix/pkg/music/sources"
)

// Sentinel errors for use-case outcomes (map to Discord copy in the command layer).
var (
	ErrMusicUnavailable   = errors.New("music service is not available")
	ErrHistoryUnavailable = errors.New("music history storage is not available")
	ErrQueueEmpty         = errors.New("no tracks left to skip")
)

const historyLinesPerPage = 15

// HistoryPageData is one page of history for the given view (no transport-specific formatting).
type HistoryPageData struct {
	View         string // normalized: "timeline" or "counts"
	TotalRows    int
	Page         int64
	TotalPages   int64
	TimelinePage []domain.MusicPlayback
	CountsPage   []domain.PlaybackCountRow
}

// Service orchestrates music use cases (slash commands delegate here).
type Service struct {
	players  PlayerProvider
	resolver TrackResolver
}

func NewService(players PlayerProvider, resolver TrackResolver) *Service {
	return &Service{players: players, resolver: resolver}
}

// TrackInfoFromMusicPlayback rebuilds resolver metadata for enqueue. Current parser is first in AvailableParsers when possible.
func TrackInfoFromMusicPlayback(m domain.MusicPlayback) sources.TrackInfo {
	parsersList := slices.Clone(m.AvailableParsers)
	if m.CurrentParser != "" {
		if i := slices.Index(parsersList, m.CurrentParser); i > 0 {
			parsersList[0], parsersList[i] = parsersList[i], parsersList[0]
		} else if i < 0 {
			parsersList = append([]string{m.CurrentParser}, parsersList...)
		}
	}
	return sources.TrackInfo{
		URL:              m.URL,
		Title:            m.Title,
		SourceName:       m.SourceName,
		AvailableParsers: parsersList,
	}
}

// Play resolves and enqueues tracks, then starts playback if idle.
func (s *Service) Play(ctx context.Context, guildID, voiceChannelID string, parsed ParsedPlayInput, source, parser string, repo domain.MusicHistoryRepository) error {
	p := s.players.GetOrCreatePlayer(guildID)
	if p == nil {
		return ErrMusicUnavailable
	}

	switch parsed.Kind {
	case PlayInputKindHistoryIDs:
		if repo == nil {
			return ErrHistoryUnavailable
		}
		for _, hid := range parsed.HistoryIDs {
			mp, err := repo.GetMusicPlayback(guildID, hid)
			if err != nil {
				return err
			}
			ti := TrackInfoFromMusicPlayback(mp)
			if err := p.EnqueueTrackInfo(ti); err != nil {
				return err
			}
		}

	case PlayInputKindURLs:
		for _, u := range parsed.URLs {
			tracks, resErr := s.resolver.Resolve(ctx, u, source, parser)
			if resErr != nil {
				return newPlayError(PlayErrorKindResolveFailed, fmt.Errorf("resolve track: %w", resErr))
			}
			if len(tracks) == 0 {
				return newPlayError(PlayErrorKindNoTracksResolved, errors.New("no tracks resolved"))
			}
			if err := p.EnqueueTrackInfo(tracks[0]); err != nil {
				return newPlayError(PlayErrorKindEnqueueTrackFailed, err)
			}
		}

	case PlayInputKindQuery:
		tracks, resErr := s.resolver.Resolve(ctx, parsed.Query, source, parser)
		if resErr != nil {
			return newPlayError(PlayErrorKindResolveFailed, fmt.Errorf("resolve track: %w", resErr))
		}
		if len(tracks) == 0 {
			return newPlayError(PlayErrorKindNoTracksResolved, errors.New("no tracks resolved"))
		}
		if err := p.EnqueueTrackInfo(tracks[0]); err != nil {
			return newPlayError(PlayErrorKindEnqueueTrackFailed, err)
		}
	}

	if !p.IsPlaying() {
		p.PlayNext(voiceChannelID)
	}
	return nil
}

// Skip moves to the next track in the queue (same semantics as /music next).
func (s *Service) Skip(guildID, voiceChannelID string) error {
	pl := s.players.GetOrCreatePlayer(guildID)
	if pl == nil {
		return ErrMusicUnavailable
	}
	if len(pl.Queue()) == 0 {
		return ErrQueueEmpty
	}
	pl.Stop(false)
	return pl.PlayNext(voiceChannelID)
}

// Stop stops playback and clears the queue (disconnect=true).
func (s *Service) Stop(guildID string) error {
	pl := s.players.GetOrCreatePlayer(guildID)
	if pl == nil {
		return ErrMusicUnavailable
	}
	return pl.Stop(true)
}

// HistoryPage selects and paginates history rows for the given view (timeline or counts).
func (s *Service) HistoryPage(guildID string, page int64, view string, repo domain.MusicHistoryRepository) (HistoryPageData, error) {
	var out HistoryPageData
	if s.players.GetOrCreatePlayer(guildID) == nil {
		return out, ErrMusicUnavailable
	}
	if repo == nil {
		return out, ErrHistoryUnavailable
	}

	view = strings.ToLower(strings.TrimSpace(view))
	if view == "" {
		view = "timeline"
	}
	out.View = view

	var fullCounts []domain.PlaybackCountRow

	switch view {
	case "counts":
		rows, err := repo.ListMusicPlaybackTimeline(guildID)
		if err != nil {
			return out, err
		}
		if len(rows) == 0 {
			return out, nil
		}
		fullCounts = domain.AggregatePlaybackCounts(rows)
		out.TotalRows = len(fullCounts)
	default:
		out.View = "timeline"
		// Timeline view can page without cloning the full history slice.
		_, total, err := repo.ListMusicPlaybackTimelinePage(guildID, 0, 0)
		if err != nil {
			return out, err
		}
		if total == 0 {
			return out, nil
		}
		out.TotalRows = total
	}

	totalPages := (out.TotalRows + historyLinesPerPage - 1) / historyLinesPerPage
	if totalPages < 1 {
		totalPages = 1
	}
	if page < 1 {
		page = 1
	}
	if int64(totalPages) > 0 && page > int64(totalPages) {
		page = int64(totalPages)
	}

	start := int((page - 1) * int64(historyLinesPerPage))
	if start >= out.TotalRows {
		start = 0
		page = 1
	}
	end := start + historyLinesPerPage
	if end > out.TotalRows {
		end = out.TotalRows
	}

	out.Page = page
	out.TotalPages = int64(totalPages)

	switch out.View {
	case "counts":
		out.CountsPage = fullCounts[start:end]
	default:
		pageRows, _, err := repo.ListMusicPlaybackTimelinePage(guildID, start, end-start)
		if err != nil {
			return HistoryPageData{}, err
		}
		out.TimelinePage = pageRows
	}
	return out, nil
}

// CurrentPlayer returns the guild player for status listeners (may be nil).
func (s *Service) CurrentPlayer(guildID string) *player.Player {
	return s.players.GetOrCreatePlayer(guildID)
}
