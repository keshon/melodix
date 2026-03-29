package music

import (
	"errors"
	"strings"

	"github.com/keshon/melodix/internal/domain"
	"github.com/keshon/melodix/internal/storage"
)

// ErrHistoryEmpty is returned when there are no persisted playback rows for the guild.
var ErrHistoryEmpty = errors.New("no playback history")

const (
	// HistoryLinesPerPage is the number of history lines per page (Discord and CLI).
	HistoryLinesPerPage = 15
	// HistoryFooterReplayDiscord is the replay hint for Discord embed footers.
	HistoryFooterReplayDiscord = "replay with `/music play <id>`."
	// HistoryFooterReplayCLI is the replay hint for CLI history output.
	HistoryFooterReplayCLI = "replay with: play <id>."
	// HistoryTitleTimelineDiscord is the embed title for timeline history (Discord).
	HistoryTitleTimelineDiscord = "🎵 Playback history (timeline)"
	// HistoryTitleCountsDiscord is the embed title for counts history (Discord).
	HistoryTitleCountsDiscord = "🎵 Playback history (by URL)"
	// HistoryTitleTimelineCLI is the plain title for timeline history (terminal).
	HistoryTitleTimelineCLI = "Playback history (timeline)"
	// HistoryTitleCountsCLI is the plain title for counts history (terminal).
	HistoryTitleCountsCLI = "Playback history (by URL)"
)

// HistoryCLIRow is one tabular row for CLI history output (tabwriter columns).
type HistoryCLIRow struct {
	ID    uint64
	Title string
	URL   string
	Tail  string // formatted date or play count
}

// HistoryPageResult is one page of formatted history lines plus metadata.
type HistoryPageResult struct {
	Title       string
	FooterExtra string // suffix after "Page x/y (z rows). "
	Lines       []string
	Rows        []HistoryCLIRow // set when BuildHistoryPage is called with includeCLIRows
	Page        int64
	TotalPages  int
	TotalRows   int
}

// BuildHistoryPage loads history, formats lines, and returns one page. footerReplayLine is appended
// to the timeline footer after "Chronological; " or used alone for counts view.
// titleTimeline and titleCounts are used as the page title for each view (Discord may use emoji; CLI should be plain ASCII).
func BuildHistoryPage(store *storage.Storage, guildID string, page int64, view string, footerReplayLine string, formatTimeline func(domain.MusicPlayback) string, formatCounts func(domain.PlaybackCountRow) string, titleTimeline, titleCounts string, includeCLIRows bool) (*HistoryPageResult, error) {
	rows, err := store.ListMusicPlaybackTimeline(guildID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrHistoryEmpty
	}

	view = strings.ToLower(strings.TrimSpace(view))
	if view == "" {
		view = "timeline"
	}

	var lines []string
	var cliRows []HistoryCLIRow
	var totalRows int
	var embedTitle string
	var footerExtra string

	switch view {
	case "counts":
		counts := domain.AggregatePlaybackCounts(rows)
		totalRows = len(counts)
		embedTitle = titleCounts
		footerExtra = footerReplayLine
		for _, r := range counts {
			lines = append(lines, formatCounts(r))
			if includeCLIRows {
				cliRows = append(cliRows, countsCLIRow(r))
			}
		}
	default:
		totalRows = len(rows)
		embedTitle = titleTimeline
		footerExtra = "Chronological; " + footerReplayLine
		for _, m := range rows {
			lines = append(lines, formatTimeline(m))
			if includeCLIRows {
				cliRows = append(cliRows, timelineCLIRow(m))
			}
		}
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
	if start >= len(lines) {
		start = 0
		page = 1
	}
	end := start + HistoryLinesPerPage
	if end > len(lines) {
		end = len(lines)
	}

	pageLines := lines[start:end]
	var pageCLIRows []HistoryCLIRow
	if includeCLIRows {
		pageCLIRows = cliRows[start:end]
	}
	return &HistoryPageResult{
		Title:       embedTitle,
		FooterExtra: footerExtra,
		Lines:       pageLines,
		Rows:        pageCLIRows,
		Page:        page,
		TotalPages:  totalPages,
		TotalRows:   totalRows,
	}, nil
}

// HistoryPageForCLI builds a history page using plain-text line formatters and CLI titles/rows.
func HistoryPageForCLI(store *storage.Storage, guildID string, page int64, view string) (*HistoryPageResult, error) {
	return BuildHistoryPage(store, guildID, page, view, HistoryFooterReplayCLI, formatTimelineLinePlain, formatCountsLinePlain, HistoryTitleTimelineCLI, HistoryTitleCountsCLI, true)
}
