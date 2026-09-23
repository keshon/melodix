// Package music is the application layer the CLI and the Discord bot share, so
// both behave identically on top of the pkg/music engine.
package music

import (
	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/storage"
	"github.com/keshon/melodix/pkg/music/cache"
	"github.com/keshon/melodix/pkg/music/parsers/ffmpeg"
	"github.com/keshon/melodix/pkg/music/parsers/kkdai"
	"github.com/keshon/melodix/pkg/music/parsers/ytdlp"
	"github.com/keshon/melodix/pkg/music/parsers/ytnative"
	"github.com/keshon/melodix/pkg/music/soundcloudapi"
	"github.com/keshon/melodix/pkg/music/stream"
	"github.com/rs/zerolog"
)

// ApplyLayers sets up the engine from config: it points the parsers' logs at
// log, sets the anti-skip read-ahead depth and, when CACHE_ENABLED, builds and
// installs the global track cache. Call once at startup, before any playback. A
// nil store still enables the cache, but its index is in-memory only — that is
// the CLI's fallback when the bot holds the data directory lock.
func ApplyLayers(cfg *config.Config, store *storage.Storage, log zerolog.Logger) error {
	kkdai.SetLogger(log)
	ffmpeg.SetLogger(log)
	soundcloudapi.SetLogger(log)
	ytnative.SetLogger(log)
	ytdlp.SetLogger(log)
	stream.SetBufferAhead(cfg.BufferAheadMs)
	ytnative.SetMaxBitrate(cfg.MaxAudioBitrate)
	if !cfg.CacheEnabled {
		// Say so out loud. A cache that is off writes nothing and logs nothing,
		// which is indistinguishable from a cache that is broken — and the usual
		// cause is a deployment that never passed CACHE_ENABLED through to the
		// process at all.
		log.Info().
			Int("buffer_ahead_ms", cfg.BufferAheadMs).
			Int("max_audio_bitrate", cfg.MaxAudioBitrate).
			Msg("track_cache_disabled")
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
		Int("max_audio_bitrate", cfg.MaxAudioBitrate).
		Msg("track_cache_enabled")
	return nil
}
