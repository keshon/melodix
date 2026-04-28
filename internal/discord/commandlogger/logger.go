package commandlogger

import (
	"log"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/internal/command"
	"github.com/keshon/melodix/internal/storage"
)

// Logger implements command.Logger so middleware can log command
// executions without importing the discord package directly.
//
// session and storage are injected once at construction — callers only supply
// the per-invocation identifiers (guildID, channelID, …).
type Logger struct {
	session *discordgo.Session
	storage *storage.Storage
}

// New creates a Logger bound to a Discord session and storage.
func New(s *discordgo.Session, store *storage.Storage) *Logger {
	return &Logger{session: s, storage: store}
}

// Ensure Logger satisfies the command.Logger interface at compile time.
var _ command.Logger = (*Logger)(nil)

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
			log.Printf("[WARN] Failed to resolve channel %s: %v", channelID, err)
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
			log.Printf("[WARN] Failed to resolve guild %s: %v", guildID, err)
			return ""
		}
	}
	return g.Name
}
