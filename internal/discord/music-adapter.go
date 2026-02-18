package discord

import (
	"fmt"

	"github.com/keshon/melodix/internal/music/player"
	"github.com/keshon/melodix/internal/music/source_resolver"
)

// GetOrCreatePlayer gets or creates a player
func (b *Bot) GetOrCreatePlayer(guildID string) *player.Player {
	b.mu.Lock()
	defer b.mu.Unlock()

	if p, ok := b.players[guildID]; ok {
		return p
	}

	if b.sourceResolver == nil {
		b.sourceResolver = source_resolver.New()
	}

	p := player.New(b.dg, guildID, b.storage, b.sourceResolver)
	b.players[guildID] = p

	return p
}

// FindUserVoiceState finds the voice state of a user
func (b *Bot) FindUserVoiceState(guildID, userID string) (*VoiceState, error) {
	guild, err := b.dg.State.Guild(guildID)
	if err != nil {
		return nil, fmt.Errorf("error retrieving guild: %w", err)
	}

	for _, vs := range guild.VoiceStates {
		if vs.UserID == userID {
			return &VoiceState{
				ChannelID: vs.ChannelID,
				UserID:    vs.UserID,
			}, nil
		}
	}
	return nil, fmt.Errorf("user not in any voice channel")
}
