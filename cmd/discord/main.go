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
	"github.com/keshon/melodix/internal/applog"

	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/discord"
	"github.com/keshon/melodix/internal/discord/command/catalog"
	"github.com/keshon/melodix/internal/discord/session"
	"github.com/keshon/melodix/internal/music"
	"github.com/keshon/melodix/internal/storage"
	"github.com/rs/zerolog"
)

func main() {
	info := buildinfo.Get()

	checkConn := flag.Bool("check", false, "connect, report what the gateway sees, and exit without registering or sending anything")
	flag.Parse()

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
	if err := music.ApplyLayers(cfg, store, log); err != nil {
		log.Fatal().Err(err).Msg("playback_layers_init_failed")
	}

	bot := discord.NewBot(cfg, store, log)

	catalog.Register(bot, log)

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
