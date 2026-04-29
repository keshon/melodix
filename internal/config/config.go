package config

import (
	"fmt"
	"os"
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

	// DiscordUnhealthyMode controls what happens when watchdogs/API probe decide the session is unhealthy.
	// Canonical: restart-session|restart-voice|ignore.
	DiscordUnhealthyMode string `env:"DISCORD_UNHEALTHY_MODE" envDefault:"restart-session"`
	// DiscordUnhealthyGrace allows ignoring the first N unhealthy signals within DiscordUnhealthyWindow
	// (still invalidating sinks), before triggering a session restart. Applies to mode=restart only.
	DiscordUnhealthyGrace int `env:"DISCORD_UNHEALTHY_GRACE" envDefault:"0"`
	// DiscordUnhealthyWindow is the counting window for DiscordUnhealthyGrace.
	DiscordUnhealthyWindow time.Duration `env:"DISCORD_UNHEALTHY_WINDOW" envDefault:"1m"`

	// PlayerTransportRecoveryMode controls how the player reacts to Discord voice transport errors.
	// Supported: hard|soft.
	PlayerTransportRecoveryMode string `env:"PLAYER_TRANSPORT_RECOVERY_MODE" envDefault:"hard"`
	// PlayerTransportSoftAttempts bounds how many "soft" retries we do before falling back to hard recovery.
	// Applies to mode=soft only.
	PlayerTransportSoftAttempts int `env:"PLAYER_TRANSPORT_SOFT_ATTEMPTS" envDefault:"1"`

	// Logging (applog / zerolog). LOG_FILE empty = stderr only (pretty console).
	LogLevel      string `env:"LOG_LEVEL" envDefault:"info"`
	LogFile       string `env:"LOG_FILE"`
	LogMaxSizeMB  int    `env:"LOG_MAX_SIZE_MB" envDefault:"10"`
	LogMaxBackups int    `env:"LOG_MAX_BACKUPS" envDefault:"3"`
	LogMaxAgeDays int    `env:"LOG_MAX_AGE_DAYS" envDefault:"0"`
	LogCompress   bool   `env:"LOG_COMPRESS" envDefault:"false"`
}

// IsDeveloper reports whether userID is the configured developer (avoids discord import in middleware).
func IsDeveloper(cfg *Config, userID string) bool {
	return cfg != nil && cfg.DeveloperID == userID
}

// New returns a new Config.
func NewConfig() (*Config, error) {
	if err := godotenv.Load(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "No .env file found, falling back to system environment variables")
	}

	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
