package storage

import "github.com/keshon/melodix/pkg/music/cache"

// cacheIndexKey is a reserved, non-guild datastore key holding the global track
// cache index. Discord guild ids are numeric snowflakes, so this never collides;
// Records() skips it.
const cacheIndexKey = "__cache_index"

// CacheIndex returns a cache.IndexStore backed by the datastore's reserved
// global key, for wiring into cache.New.
func (s *Storage) CacheIndex() cache.IndexStore { return cacheIndexStore{s} }

type cacheIndexStore struct{ s *Storage }

func (c cacheIndexStore) Load() (map[string]cache.Entry, error) {
	var m map[string]cache.Entry
	exists, err := c.s.ds.Get(cacheIndexKey, &m)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	return m, nil
}

func (c cacheIndexStore) Save(m map[string]cache.Entry) error {
	return c.s.ds.Set(cacheIndexKey, m)
}
