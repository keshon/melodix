// Package musicwire installs the optional playback layers — the anti-skip
// buffer and the global track cache — into the stream engine from config. It is
// shared by the Discord bot and the CLI so both behave identically.
package musicwire

import (
	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/storage"
	"github.com/keshon/melodix/pkg/music/cache"
	"github.com/keshon/melodix/pkg/music/stream"
	"github.com/rs/zerolog"
)

// Apply sets the anti-skip read-ahead depth and, when CACHE_ENABLED, builds and
// installs the global track cache. Call once at startup, before any playback.
// A nil store still enables the cache, but its index is in-memory only — that is
// the CLI's fallback when the bot holds the data directory lock.
func Apply(cfg *config.Config, store *storage.Storage, log zerolog.Logger) error {
	stream.SetBufferAhead(cfg.BufferAheadMs)
	if !cfg.CacheEnabled {
		return nil
	}
	var index cache.IndexStore
	if store != nil {
		index = store.CacheIndex()
	}
	c, err := cache.New(cache.Config{
		Dir:        cfg.CacheDir,
		MaxBytes:   cfg.CacheMaxBytes,
		Persistent: cfg.CachePersistent,
	}, index, log)
	if err != nil {
		return err
	}
	stream.SetCache(c)
	log.Info().
		Str("dir", cfg.CacheDir).
		Int64("max_bytes", cfg.CacheMaxBytes).
		Bool("persistent", cfg.CachePersistent).
		Bool("index_persisted", index != nil).
		Int("buffer_ahead_ms", cfg.BufferAheadMs).
		Msg("track_cache_enabled")
	return nil
}
