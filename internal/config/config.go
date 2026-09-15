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
	StoragePath           string   `env:"STORAGE_PATH" envDefault:"./data/store"` // directory the datastore owns (WAL + snapshots)
	DeveloperID           string   `env:"DEVELOPER_ID"`
	InitSlashCommands     bool     `env:"INIT_SLASH_COMMANDS" envDefault:"false"`
	// VoiceReadyDelayMs waits after joining a voice channel before asking
	// whether the channel uses end-to-end encryption. The protocol version is
	// not known until SELECT_PROTOCOL_ACK arrives, and before that the answer
	// cannot tell "no encryption here" from "not asked yet".
	VoiceReadyDelayMs int `env:"VOICE_READY_DELAY_MS" envDefault:"500"`

	// CommandParallelism limits how many command handlers run at once,
	// across every guild. Commands within one guild run one at a time
	// regardless, because a guild's music is sequential.
	//
	// There is no COMMAND_TIMEOUT beside it any more. It built a context
	// nothing consumed -- a command's Run takes the invocation data, not a
	// context, and no engine call takes one either -- so it could relabel a
	// slow command's error and nothing else. A real one means threading a
	// context from here to the parsers, which is worth doing and is not this.
	CommandParallelism int `env:"COMMAND_PARALLELISM" envDefault:"16"`
	// WSSilenceTimeout triggers a session restart if no gateway messages are
	// received.
	WSSilenceTimeout time.Duration `env:"WS_SILENCE_TIMEOUT" envDefault:"2m"`

	// DiscordUnhealthyMode controls what happens when the watchdogs or the API
	// probe decide the session is unhealthy.
	// Canonical: restart-session|restart-voice|ignore.
	DiscordUnhealthyMode string `env:"DISCORD_UNHEALTHY_MODE" envDefault:"restart-session"`
	// DiscordUnhealthyGrace allows ignoring the first N unhealthy signals within
	// DiscordUnhealthyWindow (still invalidating sinks), before triggering a
	// session restart. Applies to mode=restart only.
	DiscordUnhealthyGrace int `env:"DISCORD_UNHEALTHY_GRACE" envDefault:"0"`
	// DiscordUnhealthyWindow is the counting window for DiscordUnhealthyGrace.
	DiscordUnhealthyWindow time.Duration `env:"DISCORD_UNHEALTHY_WINDOW" envDefault:"1m"`

	// PlayerTransportRecoveryMode controls how the player reacts to Discord voice
	// transport errors. Supported: hard|soft.
	PlayerTransportRecoveryMode string `env:"PLAYER_TRANSPORT_RECOVERY_MODE" envDefault:"hard"`
	// PlayerTransportSoftAttempts bounds how many "soft" retries we do before
	// falling back to hard recovery. Applies to mode=soft only.
	PlayerTransportSoftAttempts int `env:"PLAYER_TRANSPORT_SOFT_ATTEMPTS" envDefault:"1"`

	// Track cache (opt-in): tees played Opus packets to disk so a later play of
	// the same track — any guild, or /play <history id> — is instant and
	// extraction-free.
	CacheEnabled bool `env:"CACHE_ENABLED" envDefault:"false"`
	// CacheDir holds the cache blobs and is wiped on boot when CachePersistent is
	// false.
	CacheDir string `env:"CACHE_DIR" envDefault:"./data/cache"`
	// CacheMaxBytes is the global size cap; least-recently-used tracks are evicted
	// past it.
	CacheMaxBytes int64 `env:"CACHE_MAX_BYTES" envDefault:"2147483648"` // 2 GiB
	// CachePersistent keeps the cache across restarts (false = transient, wiped on
	// boot).
	CachePersistent bool `env:"CACHE_PERSISTENT" envDefault:"true"`
	// BufferAheadMs is the anti-skip read-ahead depth in ms (0 disables). The
	// buffer sits above stream recovery, so the lead plays through a reconnect as
	// well as through short source stalls — on a lossy link this is the knob that
	// decides whether a dropped connection is audible. One buffered second is one
	// second of Opus — roughly 17 KB at YouTube's usual bitrate, so about 500 KB
	// per guild at the default depth — and it does not pre-fill, so raising it
	// delays nothing. Independent of the cache.
	BufferAheadMs int `env:"BUFFER_AHEAD_MS" envDefault:"30000"`
	// MaxAudioBitrate caps which YouTube audio format the native parser picks,
	// in bits per second. It defaults to 0, which takes the best on offer:
	// unasked, this bot plays a track at the best quality its source has.
	//
	// The cap is for links that cannot carry that. The same track is usually
	// offered near 49, 66 and 137 kbps and sometimes higher, and with
	// end-to-end encryption Discord relays whatever is sent without
	// transcoding it -- it cannot read it -- so the format chosen here is the
	// rate every listener receives. On a constrained link that is worth
	// capping, and it also halves what has to be re-fetched when a dropped
	// stream is reopened. On an ordinary one it is quality given away.
	MaxAudioBitrate int `env:"MAX_AUDIO_BITRATE" envDefault:"0"`

	// Logging (applog / zerolog). LOG_FILE empty = stderr only (pretty console).
	LogLevel      string `env:"LOG_LEVEL" envDefault:"info"`
	LogFile       string `env:"LOG_FILE"`
	LogMaxSizeMB  int    `env:"LOG_MAX_SIZE_MB" envDefault:"10"`
	LogMaxBackups int    `env:"LOG_MAX_BACKUPS" envDefault:"3"`
	LogMaxAgeDays int    `env:"LOG_MAX_AGE_DAYS" envDefault:"0"`
	LogCompress   bool   `env:"LOG_COMPRESS" envDefault:"false"`
}

// IsDeveloper reports whether userID is the configured developer (avoids
// discord import in middleware).
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
