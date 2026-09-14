// Package disgolog records command invocations to storage, resolving channel
// and guild names through disgo. It is the disgo sibling of cmdlogger.
package disgolog

import (
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/snowflake/v2"
	"github.com/rs/zerolog"

	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/storage"
)

// Logger writes who ran what to storage.
type Logger struct {
	client  *bot.Client
	storage *storage.Storage
	log     zerolog.Logger
}

// NewLogger creates a Logger bound to a disgo client and storage.
func NewLogger(client *bot.Client, store *storage.Storage, log zerolog.Logger) *Logger {
	return &Logger{client: client, storage: store, log: log}
}

var _ cmdadapter.Logger = (*Logger)(nil)

// LogCommand records a command execution, resolving names from the cache.
//
// A name that will not resolve is recorded as empty rather than failing the
// write: the row exists to say who ran what and when, and losing that because
// a channel was not cached would be the wrong trade.
func (l *Logger) LogCommand(guildID, channelID, userID, username, commandName string) error {
	return l.storage.SetCommand(
		guildID, channelID,
		l.channelName(channelID), l.guildName(guildID),
		userID, username, commandName,
	)
}

func (l *Logger) channelName(channelID string) string {
	id, err := snowflake.Parse(channelID)
	if err != nil {
		return ""
	}
	if ch, ok := l.client.Caches.Channel(id); ok {
		return ch.Name()
	}
	fetched, err := l.client.Rest.GetChannel(id)
	if err != nil {
		l.log.Warn().Str("channel_id", channelID).Err(err).Msg("channel_name_resolve_failed")
		return ""
	}
	if named, ok := fetched.(interface{ Name() string }); ok {
		return named.Name()
	}
	return ""
}

func (l *Logger) guildName(guildID string) string {
	id, err := snowflake.Parse(guildID)
	if err != nil {
		return ""
	}
	if g, ok := l.client.Caches.Guild(id); ok {
		return g.Name
	}
	fetched, err := l.client.Rest.GetGuild(id, false)
	if err != nil {
		l.log.Warn().Str("guild_id", guildID).Err(err).Msg("guild_name_resolve_failed")
		return ""
	}
	return fetched.Name
}
