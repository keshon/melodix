// Package applog configures the process-wide zerolog root (console + optional rotated JSON file).
package applog

import (
	"io"
	"os"
	"time"

	"github.com/keshon/melodix/internal/config"
	"github.com/rs/zerolog"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Setup builds a zerolog.Logger: pretty JSON lines to stderr via ConsoleWriter, and if cfg.LogFile
// is set, the same JSON events to a rotated file. service is stored on every event (e.g. discord, cli).
func Setup(service string, cfg *config.Config) zerolog.Logger {
	level, err := zerolog.ParseLevel(cfg.LogLevel)
	if err != nil {
		level = zerolog.InfoLevel
	}

	console := zerolog.ConsoleWriter{
		Out:        os.Stderr,
		TimeFormat: time.RFC3339,
	}

	var writers []io.Writer
	writers = append(writers, console)

	if cfg.LogFile != "" {
		lj := &lumberjack.Logger{
			Filename:   cfg.LogFile,
			MaxSize:    cfg.LogMaxSizeMB,
			MaxBackups: cfg.LogMaxBackups,
			MaxAge:     cfg.LogMaxAgeDays,
			Compress:   cfg.LogCompress,
		}

		if lj.MaxSize <= 0 {
			lj.MaxSize = 10
		}

		writers = append(writers, lj)
	}

	multi := io.MultiWriter(writers...)

	// КРИТИЧНО: синхронизация всего пайплайна
	sync := zerolog.SyncWriter(multi)

	return zerolog.New(sync).
		Level(level).
		With().
		Timestamp().
		Str("service", service).
		Logger()
}
