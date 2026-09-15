// Package cmdaudit records command invocations to storage, resolving channel
// and guild names from the connection.
//
// It is an audit trail, not a log. What every package writes diagnostics to is
// zerolog; this answers "who ran what, and where", and its rows outlive the
// process. Having both called Logger sent readers looking for command history
// in the wrong place.
package cmdaudit

import (
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/snowflake/v2"
	"github.com/rs/zerolog"

	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/storage"
)

// Recorder writes who ran what to storage.
type Recorder struct {
	client  *bot.Client
	storage *storage.Storage
	log     zerolog.Logger
}

// NewRecorder creates a Recorder bound to a disgo client and storage.
func NewRecorder(client *bot.Client, store *storage.Storage, log zerolog.Logger) *Recorder {
	return &Recorder{client: client, storage: store, log: log}
}

var _ cmdadapter.AuditLog = (*Recorder)(nil)

// LogCommand records a command execution, resolving names from the cache.
//
// A name that will not resolve is recorded as empty rather than failing the
// write: the row exists to say who ran what and when, and losing that because
// a channel was not cached would be the wrong trade.
func (r *Recorder) LogCommand(guildID, channelID, userID, username, commandName string) error {
	return r.storage.SetCommand(
		guildID, channelID,
		r.channelName(channelID), r.guildName(guildID),
		userID, username, commandName,
	)
}

func (r *Recorder) channelName(channelID string) string {
	id, err := snowflake.Parse(channelID)
	if err != nil {
		return ""
	}
	if ch, ok := r.client.Caches.Channel(id); ok {
		return ch.Name()
	}
	fetched, err := r.client.Rest.GetChannel(id)
	if err != nil {
		r.log.Warn().Str("channel_id", channelID).Err(err).Msg("channel_name_resolve_failed")
		return ""
	}
	if named, ok := fetched.(interface{ Name() string }); ok {
		return named.Name()
	}
	return ""
}

func (r *Recorder) guildName(guildID string) string {
	id, err := snowflake.Parse(guildID)
	if err != nil {
		return ""
	}
	if g, ok := r.client.Caches.Guild(id); ok {
		return g.Name
	}
	fetched, err := r.client.Rest.GetGuild(id, false)
	if err != nil {
		r.log.Warn().Str("guild_id", guildID).Err(err).Msg("guild_name_resolve_failed")
		return ""
	}
	return fetched.Name
}
