// Package cli implements the terminal REPL for local music playback.
// Command adapters and registration are documented in [github.com/keshon/melodix/internal/cli/command].
package cli

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	clicommand "github.com/keshon/melodix/internal/cli/command"
	"github.com/keshon/commandkit"
	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/musicapp"
	"github.com/keshon/melodix/internal/playhistory"
	"github.com/keshon/melodix/internal/storage"
	"github.com/keshon/melodix/pkg/music/player"
	"github.com/keshon/melodix/pkg/music/resolver"
	"github.com/keshon/melodix/pkg/music/sink"
)

// GuildScope is the datastore guild key for CLI playback history (distinct from Discord snowflake IDs).
const GuildScope = "cli"

// App runs the CLI REPL and coordinates playback with the shared storage layer.
type App struct {
	cfg      *config.Config
	store    *storage.Storage
	provider *sink.SpeakerProvider
	p        *player.Player
	resolver *resolver.SourceResolver
}

// New constructs a CLI app with speaker output and playback history recording.
func New(cfg *config.Config, store *storage.Storage) *App {
	provider := sink.NewSpeakerProvider()
	res := resolver.New()
	p := player.New(provider, res)
	p.SetGuildID(GuildScope)
	p.SetRecorder(playhistory.NewRecorder(store))
	return &App{cfg: cfg, store: store, provider: provider, p: p, resolver: res}
}

// Player exposes the underlying music player for tests and advanced use.
func (a *App) Player() *player.Player { return a.p }

// Store exposes storage for tests and advanced use.
func (a *App) Store() *storage.Storage { return a.store }

// Run blocks until EOF, quit, or context cancellation. It installs signal handling and prints player status.
func (a *App) Run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)

	reg := commandkit.NewRegistry()
	RegisterCLICommands(reg)

	var shutdownOnce sync.Once
	shutdown := func() {
		shutdownOnce.Do(func() {
			cancel()
			_ = a.p.Stop(true)
			_ = a.provider.Close()
			done := make(chan struct{})
			go func() {
				defer close(done)
				_ = a.store.Close()
			}()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				log.Printf("[WARN] storage Close timed out after 5s; exiting")
			}
		})
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case status, ok := <-a.p.PlayerStatus:
				if !ok {
					return
				}
				switch status {
				case player.StatusPlaying:
					if track := a.p.CurrentTrack(); track != nil {
						PrintNowPlaying(os.Stdout, track.DisplayLabel())
					}
				case player.StatusAdded:
					PrintAddedToQueue(os.Stdout)
				case player.StatusStopped:
					PrintStoppedStatus(os.Stdout)
				case player.StatusError:
					PrintPlaybackError(os.Stdout)
				}
			}
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		fmt.Println("\nShutting down...")
		shutdown()
		os.Exit(0)
	}()

	cliData := &clicommand.Data{
		Config:     a.cfg,
		Store:      a.store,
		Player:     a.p,
		Resolver:   a.resolver,
		GuildScope: GuildScope,
		Out:        os.Stdout,
		Music:      musicapp.New(a.store),
	}

	PrintStartupHelp(os.Stdout)
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
		parts := clicommand.SplitQuoted(line)
		if len(parts) == 0 {
			continue
		}
		cmdName, args := parts[0], parts[1:]

		switch cmdName {
		case "quit", "exit", "q":
			shutdown()
			os.Exit(0)
		}

		c := reg.Get(cmdName)
		if c == nil {
			fmt.Println("Unknown command. Type `help` or use: play | next | stop | queue | status | history | quit")
			continue
		}
		inv := &commandkit.Invocation{Data: cliData, Args: args}
		if err := c.Run(ctx, inv); err != nil {
			fmt.Println("Error:", err)
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("Read error: %v", err)
	}
	shutdown()
	os.Exit(0)
}
