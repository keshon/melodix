package cmdlogger

import (
	"github.com/bwmarrin/discordgo"

	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/storage"
	"github.com/rs/zerolog"
)

// Logger implements discord.Logger so middleware can log command
// executions without importing the discord package directly.
//
// session and storage are injected once at construction — callers only supply
// the per-invocation identifiers (guildID, channelID, …).
type Logger struct {
	session *discordgo.Session
	storage *storage.Storage
	log     zerolog.Logger
}

// New creates a Logger bound to a Discord session and storage.
func NewLogger(s *discordgo.Session, store *storage.Storage, log zerolog.Logger) *Logger {
	return &Logger{session: s, storage: store, log: log}
}

// Ensure Logger satisfies the discord.Logger interface at compile time.
var _ cmdadapter.Logger = (*Logger)(nil)

// LogCommand records a command execution to storage, resolving channel and guild
// names from Discord state (falling back to an API call when not cached).
func (l *Logger) LogCommand(guildID, channelID, userID, username, commandName string) error {
	channelName := l.resolveChannelName(channelID)
	guildName := l.resolveGuildName(guildID)

	return l.storage.SetCommand(guildID, channelID, channelName, guildName, userID, username, commandName)
}

func (l *Logger) resolveChannelName(channelID string) string {
	ch, err := l.session.State.Channel(channelID)
	if err != nil {
		ch, err = l.session.Channel(channelID)
		if err != nil {
			l.log.Warn().Str("channel_id", channelID).Err(err).Msg("failed to resolve channel name")
			return ""
		}
	}
	return ch.Name
}

func (l *Logger) resolveGuildName(guildID string) string {
	g, err := l.session.State.Guild(guildID)
	if err != nil {
		g, err = l.session.Guild(guildID)
		if err != nil {
			l.log.Warn().Str("guild_id", guildID).Err(err).Msg("failed to resolve guild name")
			return ""
		}
	}
	return g.Name
}
