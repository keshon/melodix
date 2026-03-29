// cmd/cli/main.go — CLI music player using the same playback engine as the Discord bot.
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/keshon/buildinfo"
	"github.com/keshon/melodix/internal/cli"
	cliui "github.com/keshon/melodix/internal/cli/ui"
	"github.com/keshon/melodix/internal/command/music"
	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/storage"
	"github.com/keshon/melodix/pkg/music/player"
	"github.com/keshon/melodix/pkg/music/resolver"
	"github.com/keshon/melodix/pkg/music/sink"
)

func main() {
	info := buildinfo.Get()
	log.Printf("[INFO] %v CLI player", info.Project)

	cfg, err := config.New()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	store, err := storage.New(cfg.StoragePath)
	if err != nil {
		log.Fatalf("Failed to open storage: %v", err)
	}

	provider := sink.NewSpeakerProvider()

	res := resolver.New()
	p := player.New(provider, res)
	p.SetGuildID(cli.GuildScope)
	p.SetRecorder(store.NewPlaybackRecorder())

	ctx, cancel := context.WithCancel(context.Background())

	var shutdownOnce sync.Once
	// shutdown cancels the UI goroutine, stops playback, releases audio, then closes storage with a bounded wait
	// so quit and signal paths match and the process does not block indefinitely on datastore autosave.
	shutdown := func() {
		shutdownOnce.Do(func() {
			cancel()
			_ = p.Stop(true)
			_ = provider.Close()
			done := make(chan struct{})
			go func() {
				defer close(done)
				_ = store.Close()
			}()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				log.Printf("[WARN] storage Close timed out after 5s; exiting")
			}
		})
	}

	// Print status updates (e.g. now playing) in the background.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case status, ok := <-p.PlayerStatus:
				if !ok {
					return
				}
				switch status {
				case player.StatusPlaying:
					if track := p.CurrentTrack(); track != nil {
						cliui.PrintNowPlaying(os.Stdout, track.DisplayLabel())
					}
				case player.StatusAdded:
					cliui.PrintAddedToQueue(os.Stdout)
				case player.StatusStopped:
					cliui.PrintStoppedStatus(os.Stdout)
				case player.StatusError:
					cliui.PrintPlaybackError(os.Stdout)
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

	cliui.PrintStartupHelp(os.Stdout)
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
			shutdown()
			os.Exit(0)
		case "help", "?":
			cliui.PrintHelpDetail(os.Stdout)
		case "play", "p":
			if len(args) == 0 {
				fmt.Println("Usage: play <url|query|id> [source] [parser]")
				continue
			}
			if err := playFromArgs(p, store, cli.GuildScope, args); err != nil {
				if errors.Is(err, music.ErrPlayInputTooManyItems) {
					fmt.Println("Error: too many items in one command (max", music.MaxPlayBatchItems, ")")
				} else {
					fmt.Println("Error:", err)
				}
				continue
			}
			if !p.IsPlaying() {
				if err := p.PlayNext(""); err != nil && err != player.ErrNoTracksInQueue {
					fmt.Println("Play error:", err)
				}
			}
		case "next", "n", "skip":
			if p.IsPlaying() {
				_ = p.Stop(false)
			}
			if err := p.PlayNext(""); err != nil {
				if err == player.ErrNoTracksInQueue {
					fmt.Println("Queue is empty")
				} else {
					fmt.Println("Error:", err)
				}
			}
		case "stop", "s":
			_ = p.Stop(true)
			fmt.Println("stopped")
		case "queue":
			cur := p.CurrentTrack()
			if cur != nil {
				fmt.Println("Now playing:", cur.DisplayLabel())
			}
			for i, t := range p.Queue() {
				fmt.Printf("  %d. %s\n", i+1, t.DisplayLabel())
			}
			if cur == nil && len(p.Queue()) == 0 {
				fmt.Println("(empty)")
			}
		case "status":
			if cur := p.CurrentTrack(); cur != nil {
				fmt.Println("Playing:", cur.DisplayLabel(), "| Queue:", len(p.Queue()))
			} else {
				fmt.Println("Stopped. Queue:", len(p.Queue()))
			}
		case "history", "h":
			view, page := parseHistoryArgs(args)
			res, err := music.HistoryPageForCLI(store, cli.GuildScope, page, view)
			if errors.Is(err, music.ErrHistoryEmpty) {
				fmt.Println("No playback history yet. Use play first. Old entries may be removed when the list is trimmed.")
				continue
			}
			if err != nil {
				fmt.Println("Error:", err)
				continue
			}
			cliui.PrintHistoryPage(os.Stdout, view, res)
		default:
			fmt.Println("Unknown command. Type `help` or use: play | next | stop | queue | status | history | quit")
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("Read error: %v", err)
	}
	shutdown()
	os.Exit(0)
}

func parseHistoryArgs(args []string) (view string, page int64) {
	view = "timeline"
	page = 1
	switch len(args) {
	case 1:
		a0 := strings.ToLower(strings.TrimSpace(args[0]))
		if a0 == "timeline" || a0 == "counts" {
			view = a0
		} else if pg, err := strconv.ParseInt(a0, 10, 64); err == nil && pg >= 1 {
			page = pg
		}
	case 2:
		a0 := strings.ToLower(strings.TrimSpace(args[0]))
		if a0 == "timeline" || a0 == "counts" {
			view = a0
			if pg, err := strconv.ParseInt(strings.TrimSpace(args[1]), 10, 64); err == nil && pg >= 1 {
				page = pg
			}
		}
	}
	return view, page
}

func playFromArgs(p *player.Player, store *storage.Storage, guildScope string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("no input")
	}

	if len(args) >= 2 && allUintStringTokens(args) {
		parsed, err := music.ParsePlayInput(strings.Join(args, " "))
		if err != nil {
			return err
		}
		if parsed.Kind == music.PlayInputKindHistoryIDs {
			return enqueueHistoryIDs(p, store, guildScope, parsed.HistoryIDs)
		}
	}

	parsed, err := music.ParsePlayInput(args[0])
	if err != nil {
		return err
	}
	if parsed.Kind == music.PlayInputKindHistoryIDs {
		return enqueueHistoryIDs(p, store, guildScope, parsed.HistoryIDs)
	}

	input := args[0]
	source, parser := "", ""
	if len(args) > 1 {
		source = args[1]
	}
	if len(args) > 2 {
		parser = args[2]
	}
	return p.Enqueue(input, source, parser)
}

func enqueueHistoryIDs(p *player.Player, store *storage.Storage, guildScope string, ids []uint64) error {
	if store == nil {
		return fmt.Errorf("music history storage is not available")
	}
	for _, hid := range ids {
		mp, gerr := store.GetMusicPlayback(guildScope, hid)
		if gerr != nil {
			if errors.Is(gerr, storage.ErrMusicPlaybackNotFound) {
				return fmt.Errorf("unknown history id %d (trimmed list or wrong id)", hid)
			}
			return fmt.Errorf("could not load history entry: %w", gerr)
		}
		ti := storage.TrackInfoFromMusicPlayback(mp)
		if err := p.EnqueueTrackInfo(ti); err != nil {
			return err
		}
	}
	return nil
}

func allUintStringTokens(ss []string) bool {
	for _, s := range ss {
		if _, err := strconv.ParseUint(s, 10, 64); err != nil {
			return false
		}
	}
	return true
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
