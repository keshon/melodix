// cmd/discord/main.go — Discord music player bot.
package main

import (
	"context"
	"flag"
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
	"github.com/keshon/melodix/internal/discord/adapter"

	"github.com/keshon/melodix/internal/command/music/history"
	"github.com/keshon/melodix/internal/command/music/next"
	"github.com/keshon/melodix/internal/command/music/play"
	"github.com/keshon/melodix/internal/command/music/queue"
	"github.com/keshon/melodix/internal/command/music/search"
	"github.com/keshon/melodix/internal/command/music/stop"

	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/discord"
	"github.com/keshon/melodix/internal/discord/session"
	"github.com/keshon/melodix/internal/middleware"
	"github.com/keshon/melodix/internal/musicwire"
	"github.com/keshon/melodix/internal/readme"
	"github.com/keshon/melodix/internal/storage"
	"github.com/rs/zerolog"
)

func main() {
	info := buildinfo.Get()

	// -readme regenerates README.md from the command registry as a dev step
	// (run from the repo root); the bot never writes files at runtime.
	genReadme := flag.Bool("readme", false, "regenerate README.md from the command registry and exit")
	checkConn := flag.Bool("check", false, "connect, report what the gateway sees, and exit without registering or sending anything")
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

	if *checkConn {
		runConnectionCheck(rootCtx, cfg, log)
		return
	}

	store, err := storage.NewStorage(cfg.StoragePath, log)
	if err != nil {
		log.Fatal().Err(err).Str("dir", cfg.StoragePath).Msg("storage_init_failed")
	}

	// Optional playback layers (cache + anti-skip buffer), set once before
	// sessions run.
	if err := musicwire.Apply(cfg, store, log); err != nil {
		log.Fatal().Err(err).Msg("playback_layers_init_failed")
	}

	bot := discord.NewBot(cfg, store, log)

	registerCommands(bot, log)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		var backoff restartBackoff
		for {
			var lastErr error
			started := time.Now()
			if err := bot.RunSession(rootCtx); err != nil {
				lastErr = err
				log.Error().Err(err).Msg("discord_session_end")
			}
			ranFor := time.Since(started)

			select {
			case <-rootCtx.Done():
				return
			default:
				delay := backoff.next(ranFor, discord.IsSessionUnhealthyError(lastErr))
				log.Warn().
					Dur("delay", delay).
					Dur("ran_for", ranFor).
					Int("consecutive_failures", backoff.failures).
					Msg("discord_session_restart")
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

	if err := store.Close(); err != nil {
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
	adapter.Register(&settings.SettingsCommand{}, mw...)
	adapter.Register(&about.About{}, mw...)
	adapter.Register(&help.Help{}, mw...)
	adapter.Register(&maintenance.Maintenance{}, mw...)
	adapter.Register(&play.Play{Bot: bot}, mw...)
	adapter.Register(&search.Search{Bot: bot}, mw...)
	adapter.Register(&next.Next{Bot: bot}, mw...)
	adapter.Register(&queue.Queue{Bot: bot}, mw...)
	adapter.Register(&stop.Stop{Bot: bot}, mw...)
	adapter.Register(&history.History{Bot: bot}, mw...)
}

// runConnectionCheck connects and reports what it can see, registering
// nothing and sending nothing.
//
// It was written to judge the disgo migration before anything had been ported
// onto it, and that reason has expired. This one has not: a compile proves
// nothing about the token, the intents, whether READY arrives, or whether REST
// authenticates -- and narrowing the gateway intents is exactly the kind of
// change whose failure mode is a gateway that refuses the connection, or a
// bot that connects and then cannot answer a command.
func runConnectionCheck(ctx context.Context, cfg *config.Config, log zerolog.Logger) {
	res, err := session.Check(ctx, cfg.DiscordToken, log)
	if err != nil {
		log.Error().Err(err).Msg("connection_check_failed")
		os.Exit(1)
	}
	log.Info().
		Str("username", res.Username).
		Int("guilds", res.Guilds).
		Dur("latency", res.Latency).
		Int("existing_commands", res.Commands).
		Str("commands_guild", res.CommandsGuild).
		Msg("connection_check_ok")
}
