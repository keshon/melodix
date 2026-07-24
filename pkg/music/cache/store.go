package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/keshon/melodix/pkg/music/opus"
	"github.com/rs/zerolog"
)

// Config controls the on-disk cache.
type Config struct {
	Dir        string // directory holding blobs
	MaxBytes   int64  // global size cap; 0 disables eviction
	Persistent bool   // false wipes the dir + index on startup
}

// Meta is the descriptive metadata stored alongside a blob.
type Meta struct {
	Source string
	Title  string
}

// Entry is one cached track (persisted in the global index).
type Entry struct {
	Key          string `json:"key"`
	File         string `json:"file"` // basename under Config.Dir
	Bytes        int64  `json:"bytes"`
	Packets      int    `json:"packets"`
	DurationMs   int64  `json:"duration_ms"`
	Source       string `json:"source"`
	Title        string `json:"title"`
	CreatedAt    int64  `json:"created_at"`     // unix nanos
	LastAccessAt int64  `json:"last_access_at"` // unix nanos; drives LRU eviction
}

// IndexStore persists the cache index (the store passes the full map on every
// change; a nil IndexStore disables persistence, e.g. in tests).
type IndexStore interface {
	Load() (map[string]Entry, error)
	Save(map[string]Entry) error
}

// Store is the global, content-keyed track cache.
type Store struct {
	cfg Config
	idx IndexStore
	log zerolog.Logger

	mu        sync.Mutex
	byKey     map[string]Entry
	total     int64
	lastStamp int64 // strictly-increasing access stamp, so LRU order is deterministic
}

// New builds a Store. Persistent=false wipes the dir and starts empty;
// Persistent=true loads the index (dropping entries whose blob is gone).
func New(cfg Config, idx IndexStore, log zerolog.Logger) (*Store, error) {
	s := &Store{cfg: cfg, idx: idx, log: log, byKey: map[string]Entry{}}
	if !cfg.Persistent {
		_ = os.RemoveAll(cfg.Dir)
	}
	if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
		return nil, err
	}
	if cfg.Persistent && idx != nil {
		if m, err := idx.Load(); err == nil {
			for k, e := range m {
				fi, statErr := os.Stat(filepath.Join(cfg.Dir, e.File))
				if statErr != nil {
					continue // blob gone; drop the stale index entry
				}
				e.Bytes = fi.Size()
				s.byKey[k] = e
				s.total += e.Bytes
				if e.LastAccessAt > s.lastStamp {
					s.lastStamp = e.LastAccessAt
				}
			}
		}
	}
	return s, nil
}

// Has reports whether a blob is cached for key.
func (s *Store) Has(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.byKey[key]
	return ok
}

// OpenAt opens the cached blob for key at the given packet offset and records an
// access (for LRU). Returns os.ErrNotExist if the key isn't cached.
func (s *Store) OpenAt(key string, seekPackets int) (opus.Reader, error) {
	s.mu.Lock()
	e, ok := s.byKey[key]
	s.mu.Unlock()
	if !ok {
		return nil, os.ErrNotExist
	}
	r, err := openBlobAt(filepath.Join(s.cfg.Dir, e.File), seekPackets)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if cur, ok := s.byKey[key]; ok {
		cur.LastAccessAt = s.stampLocked()
		s.byKey[key] = cur
		s.saveLocked()
	}
	s.mu.Unlock()
	return r, nil
}

// NewWriter opens a Writer that will cache the blob for key on Commit.
func (s *Store) NewWriter(key string, meta Meta) (*Writer, error) {
	tmp, err := os.CreateTemp(s.cfg.Dir, "tmp-*.part")
	if err != nil {
		return nil, err
	}
	w := &Writer{
		store:     s,
		key:       key,
		meta:      meta,
		tmp:       tmp,
		tmpPath:   tmp.Name(),
		finalPath: s.pathFor(key),
		bw:        newBufWriter(tmp),
	}
	if err := writeHeader(w.bw); err != nil {
		tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, err
	}
	return w, nil
}

// register records a freshly-committed blob and runs eviction.
func (s *Store) register(key, finalPath string, size int64, packets int, meta Meta) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.byKey[key]; ok {
		s.total -= old.Bytes // overwrite an existing entry
	}
	now := s.stampLocked()
	s.byKey[key] = Entry{
		Key:          key,
		File:         filepath.Base(finalPath),
		Bytes:        size,
		Packets:      packets,
		DurationMs:   int64(packets * opus.FrameMs),
		Source:       meta.Source,
		Title:        meta.Title,
		CreatedAt:    now,
		LastAccessAt: now,
	}
	s.total += size
	s.evictLocked()
	s.saveLocked()
}

// evictLocked removes least-recently-accessed entries until the total is within
// the cap. Always keeps at least one entry (the just-registered one has the
// newest stamp, so it is never the victim).
func (s *Store) evictLocked() {
	for s.cfg.MaxBytes > 0 && s.total > s.cfg.MaxBytes && len(s.byKey) > 1 {
		var victim Entry
		first := true
		for _, e := range s.byKey {
			if first || e.LastAccessAt < victim.LastAccessAt {
				victim, first = e, false
			}
		}
		delete(s.byKey, victim.Key)
		s.total -= victim.Bytes
		if err := os.Remove(filepath.Join(s.cfg.Dir, victim.File)); err != nil && !os.IsNotExist(err) {
			s.log.Warn().Err(err).Str("file", victim.File).Msg("cache_evict_remove_failed")
		}
	}
}

func (s *Store) stampLocked() int64 {
	n := time.Now().UnixNano()
	if n <= s.lastStamp {
		n = s.lastStamp + 1
	}
	s.lastStamp = n
	return n
}

func (s *Store) saveLocked() {
	if s.idx == nil {
		return
	}
	if err := s.idx.Save(s.byKey); err != nil {
		s.log.Warn().Err(err).Msg("cache_index_save_failed")
	}
}

func (s *Store) pathFor(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(s.cfg.Dir, hex.EncodeToString(sum[:])+blobExt)
}
