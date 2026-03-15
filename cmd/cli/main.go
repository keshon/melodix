// cmd/cli/main.go — CLI music player using the same playback engine as the Discord bot.
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/keshon/buildinfo"
	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/pkg/music/player"
	"github.com/keshon/melodix/pkg/music/sink"
	"github.com/keshon/melodix/pkg/music/source_resolver"
)

func main() {
	info := buildinfo.Get()
	log.Printf("[INFO] %v CLI player", info.Project)

	cfg, err := config.New()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	_ = cfg // reserved for future CLI-specific config

	provider := sink.NewSpeakerProvider()
	defer provider.Close()

	resolver := source_resolver.New()
	p := player.New(provider, resolver)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Print status updates (e.g. "Now playing: ...") in the background
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
						fmt.Println("▶", track.Title)
					}
				case player.StatusAdded:
					fmt.Println("🎶 Added to queue")
				case player.StatusStopped:
					fmt.Println("⏹ Stopped")
				case player.StatusError:
					fmt.Println("❌ Error")
				}
			}
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		fmt.Println("\nShutting down...")
		cancel()
		_ = p.Stop(true)
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
			fmt.Println("Stopped")
		case "queue":
			cur := p.CurrentTrack()
			if cur != nil {
				fmt.Println("Now playing:", cur.Title)
			}
			for i, t := range p.Queue() {
				fmt.Printf("  %d. %s\n", i+1, t.Title)
			}
			if cur == nil && len(p.Queue()) == 0 {
				fmt.Println("(empty)")
			}
		case "status":
			if cur := p.CurrentTrack(); cur != nil {
				fmt.Println("Playing:", cur.Title, "| Queue:", len(p.Queue()))
			} else {
				fmt.Println("Stopped. Queue:", len(p.Queue()))
			}
		default:
			fmt.Println("Unknown command. Use: play | next | stop | queue | status | quit")
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("Read error: %v", err)
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
