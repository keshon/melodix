// cmd/discord/main.go
package main

import (
	"context"
	"log"
	"math/rand/v2"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/keshon/buildinfo"
	"github.com/keshon/commandkit"
	"github.com/keshon/melodix/internal/command/core/about"
	"github.com/keshon/melodix/internal/command/core/commands"
	"github.com/keshon/melodix/internal/command/core/help"
	"github.com/keshon/melodix/internal/command/core/maintenance"

	"github.com/keshon/melodix/internal/command"
	"github.com/keshon/melodix/internal/command/music/history"
	"github.com/keshon/melodix/internal/command/music/next"
	"github.com/keshon/melodix/internal/command/music/play"
	"github.com/keshon/melodix/internal/command/music/stop"

	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/discord"
	"github.com/keshon/melodix/internal/middleware"
	"github.com/keshon/melodix/internal/storage"
)

func main() {
	info := buildinfo.Get()
	log.Printf("[INFO] Starting %v bot...", info.Project)

	// Root context cancels on SIGINT/SIGTERM.
	rootCtx, stopSignal := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignal()

	// Load config
	cfg, err := config.New()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	if cfg.DiscordToken == "" {
		log.Fatal("DISCORD_TOKEN is required for the Discord bot")
	}

	// Initialize storage
	store, err := storage.New(rootCtx, cfg.StoragePath)
	if err != nil {
		log.Fatal(err)
	}

	// Create bot instance
	bot := discord.NewBot(cfg, store)

	// Register commands before starting the session
	registerCommands(bot)

	// Start Discord session with auto-reconnect loop
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			var lastErr error
			if err := bot.RunSession(rootCtx); err != nil {
				lastErr = err
				log.Println("[ERR] Discord session ended:", err)
			}

			select {
			case <-rootCtx.Done():
				return
			default:
				delay := 5 * time.Second
				if discord.IsSessionUnhealthyError(lastErr) {
					// Fast restart for transient unhealthy gateway/API probe conditions.
					// Add a tiny jitter to avoid tight loops aligning with Discord infra.
					delay = time.Duration(rand.IntN(200)) * time.Millisecond
				}
				log.Printf("[WARN] Restarting session in %v...", delay)
				timer := time.NewTimer(delay)
				select {
				case <-rootCtx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		}
	}()

	<-rootCtx.Done()
	log.Println("[INFO] Shutdown signal received, stopping bot...")

	// Wait for the session loop goroutine to exit.
	wg.Wait()

	// Timebox storage shutdown so Ctrl+C always returns to the shell.
	closeCtx, cancelClose := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelClose()
	if err := store.Close(closeCtx); err != nil {
		log.Printf("[ERR] Storage close error: %v", err)
	}

	log.Println("[INFO] Discord bot exited cleanly")
}

// defaultMiddleware is the standard middleware chain applied to all commands.
var defaultMiddleware = []commandkit.Middleware{
	middleware.WithGroupAccessCheck(),
	middleware.WithGuildOnly(),
	middleware.WithUserPermissionCheck(),
	middleware.WithCommandLogger(),
}

// registerCommands registers all bot commands with the default middleware stack.
func registerCommands(bot *discord.Bot) {
	command.Register(&commands.Commands{}, defaultMiddleware...)
	command.Register(&about.About{}, defaultMiddleware...)
	command.Register(&help.Help{}, defaultMiddleware...)
	command.Register(&maintenance.Maintenance{}, defaultMiddleware...)
	command.Register(&play.Play{Bot: bot}, defaultMiddleware...)
	command.Register(&next.Next{Bot: bot}, defaultMiddleware...)
	command.Register(&stop.Stop{Bot: bot}, defaultMiddleware...)
	command.Register(&history.History{Bot: bot}, defaultMiddleware...)
}
