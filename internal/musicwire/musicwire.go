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

// Apply sets the anti-skip read-ahead depth and, when CACHE_ENABLED and a
// storage is provided, builds and installs the global track cache. Call once at
// startup, before any playback. A nil store skips the cache (buffer still set).
func Apply(cfg *config.Config, store *storage.Storage, log zerolog.Logger) error {
	stream.SetBufferAhead(cfg.BufferAheadMs)
	if !cfg.CacheEnabled || store == nil {
		return nil
	}
	c, err := cache.New(cache.Config{
		Dir:        cfg.CacheDir,
		MaxBytes:   cfg.CacheMaxBytes,
		Persistent: cfg.CachePersistent,
	}, store.CacheIndex(), log)
	if err != nil {
		return err
	}
	stream.SetCache(c)
	log.Info().
		Str("dir", cfg.CacheDir).
		Int64("max_bytes", cfg.CacheMaxBytes).
		Bool("persistent", cfg.CachePersistent).
		Int("buffer_ahead_ms", cfg.BufferAheadMs).
		Msg("track_cache_enabled")
	return nil
}
