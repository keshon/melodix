package discord

import (
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/pkg/music/player"
	"github.com/keshon/melodix/pkg/music/source_resolver"
	"github.com/keshon/melodix/pkg/music/sources"
)

// BotVoice is the interface the Discord bot exposes for voice/music commands.
type BotVoice interface {
	GetOrCreatePlayer(guildID string) *player.Player
	FindUserVoiceState(guildID, userID string) (*VoiceState, error)
	Resolve(guildID, input, source, parser string) ([]sources.TrackInfo, error)
	// UpdateGuildMusicStatus creates or edits the guild's music status message so updates work beyond 15 min token expiry.
	UpdateGuildMusicStatus(s *discordgo.Session, i *discordgo.InteractionCreate, guildID string, embed *discordgo.MessageEmbed) error
}

// VoiceState holds minimal voice channel state for a user.
type VoiceState struct {
	ChannelID string
	UserID    string
}

// GetOrCreatePlayer returns an existing player for the guild or creates a new one.
func (b *Bot) GetOrCreatePlayer(guildID string) *player.Player {
	b.mu.Lock()
	defer b.mu.Unlock()

	if p, ok := b.players[guildID]; ok {
		return p
	}
	if b.sourceResolver == nil {
		b.sourceResolver = source_resolver.New()
	}
	voiceDelay := time.Duration(b.cfg.VoiceReadyDelayMs) * time.Millisecond
	if voiceDelay <= 0 {
		voiceDelay = 500 * time.Millisecond
	}
	p := player.New(b.dg, guildID, b.sourceResolver, voiceDelay)
	b.players[guildID] = p
	return p
}

// FindUserVoiceState returns the voice channel a user is currently in, or an error if none.
func (b *Bot) FindUserVoiceState(guildID, userID string) (*VoiceState, error) {
	guild, err := b.dg.State.Guild(guildID)
	if err != nil {
		return nil, fmt.Errorf("error retrieving guild: %w", err)
	}
	for _, vs := range guild.VoiceStates {
		if vs.UserID == userID {
			return &VoiceState{ChannelID: vs.ChannelID, UserID: vs.UserID}, nil
		}
	}
	return nil, fmt.Errorf("user not in any voice channel")
}

// Resolve resolves input to tracks using the bot's shared resolver (single resolve for play flow).
func (b *Bot) Resolve(guildID, input, source, parser string) ([]sources.TrackInfo, error) {
	b.mu.Lock()
	if b.sourceResolver == nil {
		b.sourceResolver = source_resolver.New()
	}
	r := b.sourceResolver
	b.mu.Unlock()
	return r.Resolve(input, source, parser)
}

// UpdateGuildMusicStatus creates or edits the guild's music status message.
// First call uses the interaction followup and stores the message; later calls edit it (works beyond 15 min token expiry).
func (b *Bot) UpdateGuildMusicStatus(s *discordgo.Session, i *discordgo.InteractionCreate, guildID string, embed *discordgo.MessageEmbed) error {
	b.guildMusicStatusMu.RLock()
	msg, ok := b.guildMusicStatus[guildID]
	b.guildMusicStatusMu.RUnlock()

	if ok {
		_, err := s.ChannelMessageEditEmbed(msg.ChannelID, msg.MessageID, embed)
		return err
	}

	if i == nil {
		return nil // cannot create first message without interaction
	}

	m, err := s.FollowupMessageCreate(i.Interaction, false, &discordgo.WebhookParams{
		Embeds: []*discordgo.MessageEmbed{embed},
	})
	if err != nil {
		return err
	}
	if m == nil {
		return nil // Discord may not return the message in the response; followup was still sent
	}

	b.guildMusicStatusMu.Lock()
	b.guildMusicStatus[guildID] = guildMusicStatus{ChannelID: m.ChannelID, MessageID: m.ID}
	b.guildMusicStatusMu.Unlock()
	return nil
}
