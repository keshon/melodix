package history

import (
	"errors"
	"strings"

	"github.com/keshon/melodix/internal/domain"
	"github.com/keshon/melodix/internal/storage"
)

// ErrHistoryEmpty is returned when there are no persisted playback rows for the guild.
var ErrHistoryEmpty = errors.New("no playback history")

// HistoryLinesPerPage is the number of history rows per page (Discord and CLI).
const HistoryLinesPerPage = 15

// HistoryPage is one page of playback history data (timeline or aggregated counts).
type HistoryPage struct {
	View       string
	Rows       []domain.MusicPlayback
	Counts     []domain.PlaybackCountRow
	Page       int64
	TotalPages int
	TotalRows  int
}

// BuildHistoryPage loads history and returns one page of raw rows for the requested view.
func BuildHistoryPage(store *storage.Storage, guildID string, page int64, view string) (*HistoryPage, error) {
	allRows, err := store.ListMusicPlaybackTimeline(guildID)
	if err != nil {
		return nil, err
	}
	if len(allRows) == 0 {
		return nil, ErrHistoryEmpty
	}

	view = strings.ToLower(strings.TrimSpace(view))
	if view == "" {
		view = "timeline"
	}

	var totalRows int
	var outRows []domain.MusicPlayback
	var outCounts []domain.PlaybackCountRow

	switch view {
	case "counts":
		counts := domain.AggregatePlaybackCounts(allRows)
		totalRows = len(counts)
		outCounts = counts
	default:
		totalRows = len(allRows)
		outRows = allRows
	}

	totalPages := (totalRows + HistoryLinesPerPage - 1) / HistoryLinesPerPage
	if totalPages < 1 {
		totalPages = 1
	}
	if page < 1 {
		page = 1
	}
	if int64(totalPages) > 0 && page > int64(totalPages) {
		page = int64(totalPages)
	}

	start := int((page - 1) * int64(HistoryLinesPerPage))
	if start >= totalRows {
		start = 0
		page = 1
	}
	end := start + HistoryLinesPerPage
	if end > totalRows {
		end = totalRows
	}

	var pageRows []domain.MusicPlayback
	var pageCounts []domain.PlaybackCountRow
	switch view {
	case "counts":
		pageCounts = outCounts[start:end]
	default:
		pageRows = outRows[start:end]
	}

	return &HistoryPage{
		View:       view,
		Rows:       pageRows,
		Counts:     pageCounts,
		Page:       page,
		TotalPages: totalPages,
		TotalRows:  totalRows,
	}, nil
}
