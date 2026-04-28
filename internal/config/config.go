package config

import (
	"log"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

// Config is the configuration for the bot.
type Config struct {
	DiscordToken          string   `env:"DISCORD_TOKEN"` // required for Discord bot; optional for CLI
	DiscordGuildBlacklist []string `env:"DISCORD_GUILD_BLACKLIST" envSeparator:","`
	StoragePath           string   `env:"STORAGE_PATH" envDefault:"./data/datastore.json"`
	DeveloperID           string   `env:"DEVELOPER_ID"`
	InitSlashCommands     bool     `env:"INIT_SLASH_COMMANDS" envDefault:"false"`
	VoiceReadyDelayMs     int      `env:"VOICE_READY_DELAY_MS" envDefault:"500"` // VoiceReadyDelayMs is the delay in ms after joining VC before sending opus (discordgo op 4 race). Default 500.

	// CommandTimeout is a hard timebox for command execution.
	CommandTimeout time.Duration `env:"COMMAND_TIMEOUT" envDefault:"30s"`
	// CommandParallelism limits concurrently running command handlers.
	CommandParallelism int `env:"COMMAND_PARALLELISM" envDefault:"16"`
	// WSSilenceTimeout triggers a session restart if no gateway messages are received.
	WSSilenceTimeout time.Duration `env:"WS_SILENCE_TIMEOUT" envDefault:"2m"`
}

// IsDeveloper reports whether userID is the configured developer (avoids discord import in middleware).
func IsDeveloper(cfg *Config, userID string) bool {
	return cfg != nil && cfg.DeveloperID == userID
}

// New returns a new Config.
func New() (*Config, error) {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, falling back to system environment variables")
	}

	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
