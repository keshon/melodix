// cmd/discord/main.go — Discord music player bot.
package main

import (
	"context"
	"flag"
	"math/rand/v2"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/keshon/buildinfo"
	"github.com/keshon/command"
	"github.com/keshon/melodix/internal/applog"
	"github.com/keshon/melodix/internal/command/core/about"
	"github.com/keshon/melodix/internal/command/core/help"
	"github.com/keshon/melodix/internal/command/core/maintenance"
	"github.com/keshon/melodix/internal/command/settings"
	"github.com/keshon/melodix/internal/discord/cmdadapter"

	"github.com/keshon/melodix/internal/command/music/history"
	"github.com/keshon/melodix/internal/command/music/next"
	"github.com/keshon/melodix/internal/command/music/play"
	"github.com/keshon/melodix/internal/command/music/stop"

	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/discord"
	"github.com/keshon/melodix/internal/middleware"
	"github.com/keshon/melodix/internal/readme"
	"github.com/keshon/melodix/internal/storage"
	"github.com/rs/zerolog"
)

func main() {
	info := buildinfo.Get()

	// -readme regenerates README.md from the command registry as a dev step
	// (run from the repo root); the bot never writes files at runtime.
	genReadme := flag.Bool("readme", false, "regenerate README.md from the command registry and exit")
	flag.Parse()
	if *genReadme {
		log := zerolog.New(zerolog.NewConsoleWriter()).With().Timestamp().Logger()
		registerCommands(nil, log)
		if err := readme.UpdateReadme(command.DefaultRegistry, config.CategoryWeights, log); err != nil {
			log.Error().Err(err).Msg("readme_update_failed")
			os.Exit(1)
		}
		return
	}

	// Root context cancels on SIGINT/SIGTERM.
	rootCtx, stopSignal := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignal()

	cfg, err := config.NewConfig()
	if err != nil {
		_, _ = os.Stderr.WriteString("failed to load config: " + err.Error() + "\n")
		os.Exit(1)
	}

	log := applog.Setup("discord", cfg)
	log.Info().Str("project", info.Project).Msg("bot_starting")

	if cfg.DiscordToken == "" {
		log.Fatal().Msg("config_missing_token")
	}

	store, err := storage.NewStorage(rootCtx, cfg.StoragePath, log)
	if err != nil {
		log.Fatal().Err(err).Msg("storage_init_failed")
	}

	bot := discord.NewBot(cfg, store, log)

	registerCommands(bot, log)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			var lastErr error
			if err := bot.RunSession(rootCtx); err != nil {
				lastErr = err
				log.Error().Err(err).Msg("discord_session_end")
			}

			select {
			case <-rootCtx.Done():
				return
			default:
				delay := 5 * time.Second
				if discord.IsSessionUnhealthyError(lastErr) {
					delay = time.Duration(rand.IntN(200)) * time.Millisecond
				}
				log.Warn().Dur("delay", delay).Msg("discord_session_restart")
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
	log.Info().Msg("shutdown_signal_received")

	wg.Wait()

	closeCtx, cancelClose := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelClose()
	if err := store.Close(closeCtx); err != nil {
		log.Error().Err(err).Msg("storage_close_failed")
	}

	log.Info().Msg("bot_exit")
}

func defaultMiddleware(log zerolog.Logger) []command.Middleware {
	return []command.Middleware{
		middleware.WithGroupAccessCheck(),
		middleware.WithGuildOnly(),
		middleware.WithUserPermissionCheck(),
		middleware.WithCommandLogger(log),
	}
}

func registerCommands(bot *discord.Bot, log zerolog.Logger) {
	mw := defaultMiddleware(log)
	cmdadapter.Register(&settings.SettingsCommand{}, mw...)
	cmdadapter.Register(&about.About{}, mw...)
	cmdadapter.Register(&help.Help{}, mw...)
	cmdadapter.Register(&maintenance.Maintenance{}, mw...)
	cmdadapter.Register(&play.Play{Bot: bot}, mw...)
	cmdadapter.Register(&next.Next{Bot: bot}, mw...)
	cmdadapter.Register(&stop.Stop{Bot: bot}, mw...)
	cmdadapter.Register(&history.History{Bot: bot}, mw...)
}
