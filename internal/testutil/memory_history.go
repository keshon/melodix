package testutil

import (
	"slices"
	"sync"
	"time"

	"github.com/keshon/melodix/internal/domain"
)

// MemoryHistoryRepository is an in-memory domain.MusicHistoryRepository for tests.
type MemoryHistoryRepository struct {
	mu       sync.Mutex
	rows     map[string][]domain.MusicPlayback
	nextID   map[string]uint64
	trimEach int
}

// NewMemoryHistoryRepository returns an empty repository. trimEach caps rows per guild (default 750).
func NewMemoryHistoryRepository(trimEach int) *MemoryHistoryRepository {
	if trimEach <= 0 {
		trimEach = 750
	}
	return &MemoryHistoryRepository{
		rows:     make(map[string][]domain.MusicPlayback),
		nextID:   make(map[string]uint64),
		trimEach: trimEach,
	}
}

// AppendMusicPlayback implements domain.MusicHistoryRepository.
func (m *MemoryHistoryRepository) AppendMusicPlayback(guildID string, at time.Time, rec domain.MusicPlaybackAppend) (uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.nextID[guildID]++
	id := m.nextID[guildID]
	row := domain.MusicPlayback{
		ID:               id,
		PlayedAt:         at,
		URL:              rec.URL,
		Title:            rec.Title,
		CurrentParser:    rec.CurrentParser,
		AvailableParsers: slices.Clone(rec.AvailableParsers),
		SourceName:       rec.SourceName,
	}
	m.rows[guildID] = append(m.rows[guildID], row)
	if len(m.rows[guildID]) > m.trimEach {
		m.rows[guildID] = m.rows[guildID][len(m.rows[guildID])-m.trimEach:]
	}
	return id, nil
}

// GetMusicPlayback implements domain.MusicHistoryRepository.
func (m *MemoryHistoryRepository) GetMusicPlayback(guildID string, id uint64) (domain.MusicPlayback, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, row := range m.rows[guildID] {
		if row.ID == id {
			return row, nil
		}
	}
	return domain.MusicPlayback{}, domain.ErrMusicPlaybackNotFound
}

// ListMusicPlaybackTimeline implements domain.MusicHistoryRepository.
func (m *MemoryHistoryRepository) ListMusicPlaybackTimeline(guildID string) ([]domain.MusicPlayback, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.rows[guildID]), nil
}

// ListMusicPlaybackTimelinePage implements domain.MusicHistoryRepository.
func (m *MemoryHistoryRepository) ListMusicPlaybackTimelinePage(guildID string, offset, limit int) ([]domain.MusicPlayback, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	total := len(m.rows[guildID])
	if limit <= 0 || total == 0 {
		return nil, total, nil
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return []domain.MusicPlayback{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return slices.Clone(m.rows[guildID][offset:end]), total, nil
}
