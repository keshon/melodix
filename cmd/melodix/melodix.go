package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/keshon/melodix/bot"
)

const envPath = "./.env"

func main() {
	if err := godotenv.Load(envPath); err != nil {
		log.Println("Error loading .env file")
	}

	if os.Getenv("DISCORD_TOKEN") == "" {
		log.Fatal("DISCORD_TOKEN is missing in environment variables")
	}

	token := os.Getenv("DISCORD_TOKEN")
	if token == "" {
		log.Fatal("Discord token not found in environment variables")
	}

	cacheDir := "./cache"
	defer func() {
		if err := os.RemoveAll(cacheDir); err != nil {
			log.Printf("Failed to remove cache folder: %v", err)
		}
	}()

	bot, err := bot.NewBot(token)
	if err != nil {
		log.Fatal("Failed to create bot:", err)
	}
	defer bot.Shutdown()

	if err := bot.Start(); err != nil {
		log.Fatal("Failed to start bot:", err)
	}

	fmt.Println("Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc
	close(sc)
}
