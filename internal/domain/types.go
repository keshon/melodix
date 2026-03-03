package domain

import (
	"time"
)

type CommandHistory struct {
	ChannelID   string    `json:"channel_id"`
	ChannelName string    `json:"channel_name"`
	GuildName   string    `json:"guild_name"`
	UserID      string    `json:"user_id"`
	Username    string    `json:"username"`
	Command     string    `json:"command"`
	Datetime    time.Time `json:"datetime"`
}

type Record struct {
	CommandsDisabled []string         `json:"commands_disabled"`
	CommandsHistory  []CommandHistory `json:"commands_history"`
}
