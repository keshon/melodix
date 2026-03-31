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
	"github.com/keshon/melodix/internal/command/core"
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

	// Load config
	cfg, err := config.New()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	if cfg.DiscordToken == "" {
		log.Fatal("DISCORD_TOKEN is required for the Discord bot")
	}

	// Initialize storage
	store, err := storage.New(cfg.StoragePath)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	// Create bot instance
	bot := discord.NewBot(cfg, store)

	// Register commands before starting the session
	registerCommands(bot)

	// Context for shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start Discord session with auto-reconnect loop
	go func() {
		for {
			if err := bot.RunSession(ctx); err != nil {
				log.Println("[ERR] Discord session ended:", err)
			}

			select {
			case <-ctx.Done():
				return
			default:
				log.Println("[WARN] Restarting session in 5s...")
				time.Sleep(5 * time.Second)
			}
		}
	}()

	// Handle OS signals
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	<-sig
	log.Println("[INFO] Shutdown signal received, stopping bot...")
	cancel()

	// Give goroutines time to clean up
	time.Sleep(1 * time.Second)
	log.Println("[INFO] Discord bot exited cleanly")
}

// registerCommands registers all commands with middleware
func registerCommands(bot *discord.Bot) {
	command.RegisterCommand(
		&core.CommandsCommand{},
		middleware.WithGroupAccessCheck(),
		middleware.WithGuildOnly(),
		middleware.WithUserPermissionCheck(),
		middleware.WithCommandLogger(),
	)

	command.RegisterCommand(
		&core.AboutCommand{},
		middleware.WithGroupAccessCheck(),
		middleware.WithGuildOnly(),
		middleware.WithUserPermissionCheck(),
		middleware.WithCommandLogger(),
	)

	command.RegisterCommand(
		&core.HelpUnifiedCommand{},
		middleware.WithGroupAccessCheck(),
		middleware.WithGuildOnly(),
		middleware.WithUserPermissionCheck(),
		middleware.WithCommandLogger(),
	)

	command.RegisterCommand(
		&core.MaintenanceCommand{},
		middleware.WithGroupAccessCheck(),
		middleware.WithGuildOnly(),
		middleware.WithUserPermissionCheck(),
		middleware.WithCommandLogger(),
	)

	command.RegisterCommand(
		&music.MusicCommand{Bot: bot},
		middleware.WithGroupAccessCheck(),
		middleware.WithGuildOnly(),
		middleware.WithUserPermissionCheck(),
		middleware.WithCommandLogger(),
	)
}
