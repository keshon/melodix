// cmd/cli/main.go — the terminal frontend: a music player on the same service
// as the Discord bot, playing to the local speaker.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/keshon/buildinfo"
	"github.com/keshon/datastore"

	"github.com/keshon/melodix/internal/applog"
	"github.com/keshon/melodix/internal/cli"
	"github.com/keshon/melodix/internal/cli/command/catalog"
	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/music"
	"github.com/keshon/melodix/internal/storage"
	"github.com/keshon/melodix/pkg/music/sink"
)

// scope is the terminal's one player, and the key its plays are recorded
// under in playback history.
const scope = "cli"

func main() {
	info := buildinfo.Get()

	cfg, err := config.NewConfig()
	if err != nil {
		_, _ = os.Stderr.WriteString("failed to load config: " + err.Error() + "\n")
		os.Exit(1)
	}

	log := applog.Setup("cli", cfg)
	log.Info().Str("project", info.Project).Msg("cli_starting")

	// Storage keeps playback history and the cache index. The data directory
	// takes an exclusive lock, so when the bot already holds it the CLI plays
	// on without history and with an in-memory cache index instead of refusing
	// to start.
	store, err := storage.NewStorage(cfg.StoragePath, log)
	switch {
	case errors.Is(err, datastore.ErrLocked):
		log.Warn().Str("dir", cfg.StoragePath).Msg("storage_locked_by_another_process_running_without_history")
		store = nil
	case err != nil:
		log.Warn().Err(err).Str("dir", cfg.StoragePath).Msg("storage_init_failed_running_without_history")
		store = nil
	default:
		defer func() { _ = store.Close() }()
	}
	if err := music.ApplyLayers(cfg, store, log); err != nil {
		log.Fatal().Err(err).Msg("playback_layers_init_failed")
	}

	provider := sink.NewSpeakerProviderWithLogger(log)
	defer provider.Close()
	svc := music.New(cfg, store, log, music.Hooks{
		NewSink:  func(string) sink.Provider { return provider },
		Watch:    cli.StatusPrinter(os.Stdout),
		OnFailed: cli.FailurePrinter(os.Stdout),
	})
	defer svc.StopAll()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		fmt.Println("\nShutting down...")
		svc.StopAll()
		os.Exit(0)
	}()

	reg := cli.NewRegistry()
	catalog.Register(reg)
	if err := cli.Run(reg, svc, scope, os.Stdin, os.Stdout); err != nil {
		log.Error().Err(err).Msg("cli_stdin_error")
	}
}
