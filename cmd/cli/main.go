// cmd/cli/main.go — CLI music player using the same playback engine as the Discord bot.
package main

import (
	"context"
	"log"

	"github.com/keshon/buildinfo"
	"github.com/keshon/melodix/internal/cli"
	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/storage"
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

	app := cli.New(cfg, store)
	app.Run(context.Background())
}
