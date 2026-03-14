// cmd/discord/main.go
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/keshon/buildinfo"
	_ "github.com/keshon/melodix/internal/command/core"

	"github.com/keshon/melodix/internal/command"
	"github.com/keshon/melodix/internal/command/music"

	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/discord"
	"github.com/keshon/melodix/internal/middleware"
	"github.com/keshon/melodix/internal/storage"
)

func main() {
	info := buildinfo.Get()

	log.Printf("[INFO] Starting %v bot...", info.Project)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg, err := config.New()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	store, err := storage.New(cfg.StoragePath)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	bot := discord.NewBot(cfg, store)
	command.RegisterCommand(
		&music.MusicCommand{Bot: bot},
		middleware.WithGroupAccessCheck(),
		middleware.WithGuildOnly(),
		middleware.WithUserPermissionCheck(),
		middleware.WithCommandLogger(),
	)

	errCh := make(chan error, 1)
	go func() {
		if err := bot.Run(ctx); err != nil {
			errCh <- err
		}
		close(errCh)
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	select {
	case s := <-sig:
		log.Printf("[INFO] Received signal %s, shutting down...\n", s)
		cancel()
	case err := <-errCh:
		if err != nil {
			log.Println("[ERR] Discord bot error:", err)
		}
		cancel()
	case <-ctx.Done():
	}

	// Force-exit if cleanup takes too long (e.g. blocked voice disconnect,
	// storage close, or lingering goroutines).
	go func() {
		time.Sleep(1 * time.Second)
		log.Println("[WARN] Cleanup timed out, forcing exit")
		os.Exit(1)
	}()

	// Wait for the bot goroutine to exit so defer dg.Close() and cleanup run before process exit.
	<-errCh
	log.Println("[INFO] Discord bot exited cleanly")
}
