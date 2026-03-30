package discord

import (
	"context"
	"fmt"

	"github.com/bwmarrin/discordgo"
	appmusic "github.com/keshon/melodix/internal/app/music"
	"github.com/keshon/melodix/pkg/music/player"
	"github.com/keshon/melodix/pkg/music/sources"
)

// MusicPresenter updates the guild 'now playing' status message (Discord API).
type MusicPresenter interface {
	UpdateGuildMusicStatus(s *discordgo.Session, i *discordgo.InteractionCreate, guildID string, embed *discordgo.MessageEmbed) error
}

// VoiceStateFinder is a Discord-only capability: locate a user's active voice channel.
type VoiceStateFinder interface {
	FindUserVoiceState(guildID, userID string) (*appmusic.VoiceState, error)
}

// DiscordMusicBot combines app voice capabilities with Discord-only status UI.
type DiscordMusicBot interface {
	appmusic.PlayerProvider
	appmusic.TrackResolver
	VoiceStateFinder
	MusicPresenter
}

var (
	_ appmusic.PlayerProvider = (*Bot)(nil)
	_ appmusic.TrackResolver  = (*Bot)(nil)
	_ VoiceStateFinder        = (*Bot)(nil)
	_ MusicPresenter          = (*Bot)(nil)
	_ DiscordMusicBot         = (*Bot)(nil)
)

// GetOrCreatePlayer returns an existing player for the guild or creates a new one (delegates to voice service).
func (b *Bot) GetOrCreatePlayer(guildID string) *player.Player {
	if b.voice == nil {
		return nil
	}
	return b.voice.GetOrCreatePlayer(guildID)
}

// FindUserVoiceState returns the voice channel a user is currently in, or an error if none.
func (b *Bot) FindUserVoiceState(guildID, userID string) (*appmusic.VoiceState, error) {
	guild, err := b.dg.State.Guild(guildID)
	if err != nil {
		return nil, fmt.Errorf("error retrieving guild: %w", err)
	}
	for _, vs := range guild.VoiceStates {
		if vs.UserID == userID {
			return &appmusic.VoiceState{ChannelID: vs.ChannelID, UserID: vs.UserID}, nil
		}
	}
	return nil, fmt.Errorf("user not in any voice channel")
}

// Resolve resolves input to tracks using the bot's shared resolver (delegates to voice service).
func (b *Bot) Resolve(ctx context.Context, input, source, parser string) ([]sources.TrackInfo, error) {
	if b.voice == nil {
		return nil, fmt.Errorf("voice service not available")
	}
	return b.voice.Resolve(ctx, input, source, parser)
}

// UpdateGuildMusicStatus creates or edits the guild's music status message (delegates to voice service).
func (b *Bot) UpdateGuildMusicStatus(s *discordgo.Session, i *discordgo.InteractionCreate, guildID string, embed *discordgo.MessageEmbed) error {
	if b.voice == nil {
		return nil
	}
	return b.voice.UpdateGuildMusicStatus(s, i, guildID, embed)
}
