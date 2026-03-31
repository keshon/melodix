package voice

import (
	"log"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/storage"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/player"
	"github.com/keshon/melodix/pkg/music/resolve"
	"github.com/keshon/melodix/pkg/music/sources"
)

type guildMusicStatus struct {
	ChannelID string
	MessageID string
}

// Service provides voice/music for a Discord bot: players, sink providers, resolver, and guild music status.
// It is pluggable: a bot without voice can omit it.
type Service struct {
	getSession    SessionGetter
	cfg           *config.Config
	store         *storage.Storage
	mu            sync.RWMutex
	players       map[string]*player.Player
	sinkProviders map[string]*DiscordSinkProvider
	resolver      *resolve.Resolver

	guildMusicStatus   map[string]guildMusicStatus
	guildMusicStatusMu sync.RWMutex
}

// New creates a voice service for the given session getter and config.
func New(getSession SessionGetter, cfg *config.Config, store *storage.Storage) *Service {
	return &Service{
		getSession:       getSession,
		cfg:              cfg,
		store:            store,
		players:          make(map[string]*player.Player),
		sinkProviders:    make(map[string]*DiscordSinkProvider),
		guildMusicStatus: make(map[string]guildMusicStatus),
	}
}

type playbackRecorder struct {
	store *storage.Storage
}

func (r playbackRecorder) Record(guildID string, playedAt time.Time, track parsers.TrackParse) {
	if r.store == nil {
		return
	}
	if _, err := r.store.AppendMusicPlayback(guildID, track, playedAt); err != nil {
		log.Printf("[music] append playback history: %v", err)
	}
}

// GetOrCreatePlayer returns an existing player for the guild or creates a new one.
func (s *Service) GetOrCreatePlayer(guildID string) *player.Player {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sinkProviders == nil {
		s.sinkProviders = make(map[string]*DiscordSinkProvider)
	}
	if p, ok := s.players[guildID]; ok {
		p.SetGuildID(guildID)
		if s.store != nil {
			p.SetRecorder(playbackRecorder{store: s.store})
		}
		return p
	}
	if s.resolver == nil {
		s.resolver = resolve.New()
	}
	provider, ok := s.sinkProviders[guildID]
	if !ok {
		voiceDelay := time.Duration(s.cfg.VoiceReadyDelayMs) * time.Millisecond
		provider = NewDiscordSinkProvider(s.getSession, guildID, voiceDelay)
		s.sinkProviders[guildID] = provider
	}
	p := player.New(provider, s.resolver)
	p.SetGuildID(guildID)
	if s.store != nil {
		p.SetRecorder(playbackRecorder{store: s.store})
	}
	s.players[guildID] = p
	return p
}

// Resolve resolves input to tracks using the service's shared resolver.
func (s *Service) Resolve(guildID, input, source, parser string) ([]sources.TrackInfo, error) {
	s.mu.Lock()
	if s.resolver == nil {
		s.resolver = resolve.New()
	}
	r := s.resolver
	s.mu.Unlock()
	return r.Resolve(input, source, parser)
}

// UpdateGuildMusicStatus creates or edits the guild's music status message.
func (s *Service) UpdateGuildMusicStatus(session *discordgo.Session, i *discordgo.InteractionCreate, guildID string, embed *discordgo.MessageEmbed) error {
	s.guildMusicStatusMu.RLock()
	msg, ok := s.guildMusicStatus[guildID]
	s.guildMusicStatusMu.RUnlock()

	if ok {
		_, err := session.ChannelMessageEditEmbed(msg.ChannelID, msg.MessageID, embed)
		return err
	}

	if i == nil {
		return nil
	}

	m, err := session.FollowupMessageCreate(i.Interaction, false, &discordgo.WebhookParams{
		Embeds: []*discordgo.MessageEmbed{embed},
	})
	if err != nil {
		return err
	}
	if m == nil {
		return nil
	}

	s.guildMusicStatusMu.Lock()
	s.guildMusicStatus[guildID] = guildMusicStatus{ChannelID: m.ChannelID, MessageID: m.ID}
	s.guildMusicStatusMu.Unlock()
	return nil
}

// StopAllPlayers stops playback and disconnects voice for all guilds. Call on shutdown.
func (s *Service) StopAllPlayers() {
	s.mu.Lock()
	players := make(map[string]*player.Player, len(s.players))
	for k, v := range s.players {
		players[k] = v
	}
	s.players = make(map[string]*player.Player)
	s.sinkProviders = nil // reinitialized on next GetOrCreatePlayer if needed
	s.mu.Unlock()

	for _, p := range players {
		_ = p.Stop(true)
	}
}
