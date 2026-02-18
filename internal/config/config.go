package config

import (
	"log"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

// Config is the configuration for the bot.
type Config struct {
	DiscordToken          string   `env:"DISCORD_TOKEN,required"`
	DiscordGuildBlacklist []string `env:"DISCORD_GUILD_BLACKLIST" envSeparator:","`
	StoragePath           string   `env:"STORAGE_PATH" envDefault:"./data/datastore.json"`
	InitSlashCommands     bool     `env:"INIT_SLASH_COMMANDS" envDefault:"false"`
}

// New returns a new Config.
func New() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, falling back to system environment variables")
	}

	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	return &cfg
}
