package musicapp

import (
	"github.com/keshon/melodix/internal/history"
)

// HistoryPage is one page of playback history (facade view of the history package).
type HistoryPage = history.HistoryPage

// ErrHistoryEmpty is returned when there is no persisted history for the guild.
var ErrHistoryEmpty = history.ErrHistoryEmpty
