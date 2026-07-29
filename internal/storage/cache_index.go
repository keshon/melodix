package storage

import "github.com/keshon/melodix/pkg/music/cache"

// CacheIndex returns a cache.IndexStore backed by the cache_entries collection,
// for wiring into cache.New. Entries are written one at a time, so caching a
// track appends a single small record.
func (s *Storage) CacheIndex() cache.IndexStore { return cacheIndexStore{s} }

type cacheIndexStore struct{ s *Storage }

func (c cacheIndexStore) Load() (map[string]cache.Entry, error) {
	out := make(map[string]cache.Entry, c.s.cacheIdx.Len())
	for e := range c.s.cacheIdx.All() {
		out[e.ID] = e.Entry
	}
	return out, nil
}

func (c cacheIndexStore) Put(e cache.Entry) error {
	return c.s.cacheIdx.Put(&CacheEntry{Entry: e})
}

func (c cacheIndexStore) Delete(id string) error {
	return c.s.cacheIdx.Delete(id)
}
