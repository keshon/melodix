// cmd/cli/main.go — CLI music player using the same playback engine as the
// Discord bot.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/keshon/buildinfo"
	"github.com/keshon/datastore"
	"github.com/keshon/melodix/internal/applog"
	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/music"
	"github.com/keshon/melodix/internal/storage"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/player"
	"github.com/keshon/melodix/pkg/music/sink"
)

func main() {
	info := buildinfo.Get()

	cfg, err := config.NewConfig()
	if err != nil {
		_, _ = os.Stderr.WriteString("failed to load config: " + err.Error() + "\n")
		os.Exit(1)
	}

	log := applog.Setup("cli", cfg)
	log.Info().Str("project", info.Project).Msg("cli_starting")

	provider := sink.NewSpeakerProviderWithLogger(log)
	defer provider.Close()

	// Optional playback layers (cache + anti-skip buffer), shared with the bot.
	// Storage is only needed to persist the cache index. The data directory takes
	// an exclusive lock, so when the bot already holds it the CLI keeps playing
	// with an in-memory cache index instead of refusing to start.
	var store *storage.Storage
	if cfg.CacheEnabled {
		store, err = storage.NewStorage(cfg.StoragePath, log)
		switch {
		case errors.Is(err, datastore.ErrLocked):
			log.Warn().Str("dir", cfg.StoragePath).
				Msg("storage_locked_by_another_process_cache_index_not_persisted")
			store = nil
		case err != nil:
			log.Fatal().Err(err).Str("dir", cfg.StoragePath).Msg("storage_init_failed")
		default:
			defer func() { _ = store.Close() }()
		}
	}
	if err := music.ApplyLayers(cfg, store, log); err != nil {
		log.Fatal().Err(err).Msg("playback_layers_init_failed")
	}

	svc := music.New(cfg, store, log, music.Hooks{
		NewSink:  func(string) sink.Provider { return provider },
		Watch:    printStatus,
		OnFailed: func(_ string, track parsers.Track, err error) { fmt.Println("❌", track.Title+":", err) },
	})
	p := svc.Player(scope)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		fmt.Println("\nShutting down...")
		svc.StopAll()
		os.Exit(0)
	}()

	fmt.Println("Commands: play <url|query> [source] [parser] | next | stop | queue | status | quit")
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := splitQuoted(line)
		if len(parts) == 0 {
			continue
		}
		cmd, args := parts[0], parts[1:]
		switch cmd {
		case "quit", "exit", "q":
			_ = p.Stop(true)
			return
		case "play", "p":
			if len(args) == 0 {
				fmt.Println("Usage: play <url|query> [source] [parser]")
				continue
			}
			input := args[0]
			source, parser := "", ""
			if len(args) > 1 {
				source = args[1]
			}
			if len(args) > 2 {
				parser = args[2]
			}
			if err := p.Enqueue(input, source, parser); err != nil {
				fmt.Println("Error:", err)
				continue
			}
			if !p.IsPlaying() {
				if err := p.PlayNext(""); err != nil && !errors.Is(err, player.ErrNoTracksInQueue) {
					fmt.Println("Play error:", err)
				}
			}
		case "next", "n", "skip":
			if p.IsPlaying() {
				_ = p.Stop(false)
			}
			if err := p.PlayNext(""); err != nil {
				if errors.Is(err, player.ErrNoTracksInQueue) {
					fmt.Println("Queue is empty")
				} else {
					fmt.Println("Error:", err)
				}
			}
		case "stop", "s":
			_ = p.Stop(true)
			fmt.Println("Stopped")
		case "queue":
			cur, playing := p.CurrentTrack()
			if playing {
				fmt.Println("Now playing:", cur.Title)
			}
			for i, t := range p.Queue() {
				fmt.Printf("  %d. %s\n", i+1, t.Title)
			}
			if !playing && len(p.Queue()) == 0 {
				fmt.Println("(empty)")
			}
		case "status":
			if cur, ok := p.CurrentTrack(); ok {
				fmt.Println("Playing:", cur.Title, "| Queue:", len(p.Queue()))
			} else {
				fmt.Println("Stopped. Queue:", len(p.Queue()))
			}
		default:
			fmt.Println("Unknown command. Use: play | next | stop | queue | status | quit")
		}
	}
	if err := scanner.Err(); err != nil {
		log.Error().Err(err).Msg("cli_stdin_error")
	}
}

// scope is the CLI's one player, and the key its plays are recorded under.
const scope = "cli"

// printStatus is the player's single status consumer: it prints each change
// as it happens.
func printStatus(_ string, p *player.Player) {
	for status := range p.PlayerStatus {
		switch status {
		case player.StatusPlaying:
			if track, ok := p.CurrentTrack(); ok {
				fmt.Println("▶", track.Title)
			}
		case player.StatusAdded:
			fmt.Println("🎶 Added to queue")
		case player.StatusStopped:
			fmt.Println("⏹ Stopped")
		case player.StatusError:
			fmt.Println("❌ Error")
		case player.StatusPaused, player.StatusResumed:
			// The player supports neither.
		}
	}
}

// splitQuoted splits the line by spaces but keeps quoted segments as one token.
func splitQuoted(s string) []string {
	var out []string
	var buf strings.Builder
	inQuote := false
	for _, r := range s {
		switch {
		case r == '"' || r == '\'':
			inQuote = !inQuote
		case (r == ' ' || r == '\t') && !inQuote:
			if buf.Len() > 0 {
				out = append(out, buf.String())
				buf.Reset()
			}
		default:
			buf.WriteRune(r)
		}
	}
	if buf.Len() > 0 {
		out = append(out, buf.String())
	}
	return out
}
