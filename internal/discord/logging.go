package discord

import (
	"log"

	"github.com/keshon/melodix/internal/storage"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/internal/command"
)

// cmdLogger implements command.CommandLogger so middleware can log without importing discord.
type cmdLogger struct{}

func (cmdLogger) LogCommand(s *discordgo.Session, store *storage.Storage, guildID, channelID, userID, username, commandName string) error {
	return LogCommand(s, store, guildID, channelID, userID, username, commandName)
}

// DefaultLogger is injected into command contexts.
var DefaultLogger command.CommandLogger = cmdLogger{}

// LogCommand records a command execution to storage, resolving channel and guild names from state.
func LogCommand(s *discordgo.Session, store *storage.Storage, guildID, channelID, userID, username, commandName string) error {
	channel, err := s.State.Channel(channelID)
	if err != nil {
		channel, err = s.Channel(channelID)
		if err != nil {
			log.Println("[WARN] Failed to fetch channel:", err)
		}
	}
	channelName := ""
	if channel != nil {
		channelName = channel.Name
	}

	guild, err := s.State.Guild(guildID)
	if err != nil {
		guild, err = s.Guild(guildID)
		if err != nil {
			log.Println("[WARN] Failed to fetch guild:", err)
		}
	}
	guildName := ""
	if guild != nil {
		guildName = guild.Name
	}

	return store.SetCommand(guildID, channelID, channelName, guildName, userID, username, commandName)
}
