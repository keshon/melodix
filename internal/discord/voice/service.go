package voice

import (
	"fmt"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/discord/discordreply"
	"github.com/keshon/melodix/internal/discord/voice/sink"
	"github.com/keshon/melodix/internal/playbackerr"
	"github.com/keshon/melodix/internal/storage"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/player"
	"github.com/keshon/melodix/pkg/music/resolve"
	"github.com/keshon/melodix/pkg/music/sources"
	"github.com/rs/zerolog"
)

// SessionGetter returns the current Discord session (used so providers stay valid across reconnects).
//
// Kept in the parent package so call sites (e.g. bot wiring) keep using voice.New(...)
// without importing the implementation subpackage.
type SessionGetter = sink.SessionGetter

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
	log           zerolog.Logger
	mu            sync.RWMutex
	players       map[string]*player.Player
	sinkProviders map[string]*sink.DiscordSinkProvider
	resolver      *resolve.Resolver

	guildMusicStatus map[string]guildMusicStatus
	// guildMusicNotifyChannel is the text channel of the last music slash (/play, /next, …) for fallback
	// "Playback failed" when no status message id is stored yet or edit fails.
	guildMusicNotifyChannel map[string]string
	guildMusicStatusMu      sync.RWMutex
}

// New creates a voice service for the given session getter and config.
func NewVoiceService(getSession SessionGetter, cfg *config.Config, store *storage.Storage, log zerolog.Logger) *Service {
	return &Service{
		getSession:              getSession,
		cfg:                     cfg,
		store:                   store,
		log:                     log,
		players:                 make(map[string]*player.Player),
		sinkProviders:           make(map[string]*sink.DiscordSinkProvider),
		guildMusicStatus:        make(map[string]guildMusicStatus),
		guildMusicNotifyChannel: make(map[string]string),
	}
}

type playbackRecorder struct {
	store *storage.Storage
	log   zerolog.Logger
}

func (r playbackRecorder) Record(guildID string, playedAt time.Time, track parsers.TrackParse) {
	if r.store == nil {
		return
	}
	if _, err := r.store.AppendMusicPlayback(guildID, track, playedAt); err != nil {
		r.log.Warn().Str("guild_id", guildID).Err(err).Msg("playback_history_append_failed")
	}
}

func (s *Service) attachPlaybackFailureNotifier(p *player.Player) {
	p.SetOnPlaybackFailed(func(guildID string, track parsers.TrackParse, err error) {
		sess := s.getSession()
		if sess == nil {
			return
		}
		detail := playbackerr.String(err.Error())
		var desc string
		if track.Title != "" && track.URL != "" {
			desc = fmt.Sprintf("%s\n\n[%s](%s)", detail, track.Title, track.URL)
		} else if track.Title != "" {
			desc = fmt.Sprintf("%s\n\n%s", detail, track.Title)
		} else {
			desc = detail
		}
		s.deliverPlaybackFailureEmbed(sess, guildID, &discordgo.MessageEmbed{
			Title:       "Playback failed",
			Description: desc,
			Color:       discordreply.EmbedColor,
		})
	})
}

// deliverPlaybackFailureEmbed edits the stored "now playing" message when possible; otherwise sends
// a public embed to the last known slash channel (see SetGuildMusicNotifyChannel / UpdatePlaybackStatus).
func (s *Service) deliverPlaybackFailureEmbed(session *discordgo.Session, guildID string, embed *discordgo.MessageEmbed) {
	s.guildMusicStatusMu.RLock()
	msg, hasMsg := s.guildMusicStatus[guildID]
	notifyCh := s.guildMusicNotifyChannel[guildID]
	s.guildMusicStatusMu.RUnlock()

	if hasMsg && msg.ChannelID != "" && msg.MessageID != "" {
		if _, err := session.ChannelMessageEditEmbed(msg.ChannelID, msg.MessageID, embed); err != nil {
			s.log.Warn().Str("guild_id", guildID).Err(err).Msg("playback_failed_embed_edit_failed")
			if notifyCh != "" {
				if _, err2 := session.ChannelMessageSendEmbed(notifyCh, embed); err2 != nil {
					s.log.Warn().Str("guild_id", guildID).Str("channel_id", notifyCh).Err(err2).Msg("playback_failed_fallback_send_failed")
				} else {
					s.log.Info().Str("guild_id", guildID).Str("channel_id", notifyCh).Msg("playback_failed_sent_after_edit_failed")
				}
			}
		}
		return
	}

	if notifyCh != "" {
		if _, err := session.ChannelMessageSendEmbed(notifyCh, embed); err != nil {
			s.log.Warn().Str("guild_id", guildID).Str("channel_id", notifyCh).Err(err).Msg("playback_failed_channel_send_failed")
		} else {
			s.log.Info().Str("guild_id", guildID).Str("channel_id", notifyCh).Msg("playback_failed_sent_public_fallback")
		}
		return
	}

	s.log.Warn().Str("guild_id", guildID).Msg("playback_failed_no_ui_target")
}

// GetOrCreatePlayer returns an existing player for the guild or creates a new one.
func (s *Service) GetOrCreatePlayer(guildID string) *player.Player {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sinkProviders == nil {
		s.sinkProviders = make(map[string]*sink.DiscordSinkProvider)
	}
	if p, ok := s.players[guildID]; ok {
		p.SetGuildID(guildID)
		s.attachPlaybackFailureNotifier(p)
		if s.store != nil {
			p.SetRecorder(playbackRecorder{store: s.store, log: s.log})
		}
		return p
	}
	if s.resolver == nil {
		s.resolver = resolve.New()
	}
	provider, ok := s.sinkProviders[guildID]
	if !ok {
		voiceDelay := time.Duration(s.cfg.VoiceReadyDelayMs) * time.Millisecond
		provider = sink.NewDiscordSinkProvider(s.getSession, guildID, voiceDelay, s.log)
		s.sinkProviders[guildID] = provider
	}
	p := player.NewWithOptions(provider, s.resolver, player.Options{
		Logger:                s.log,
		TransportRecoveryMode: s.cfg.PlayerTransportRecoveryMode,
		TransportSoftAttempts: s.cfg.PlayerTransportSoftAttempts,
	})
	s.attachPlaybackFailureNotifier(p)
	p.SetGuildID(guildID)
	if s.store != nil {
		p.SetRecorder(playbackRecorder{store: s.store, log: s.log})
	}
	s.players[guildID] = p
	return p
}

// ResolveTracks resolves input to tracks using the service's shared resolver.
func (s *Service) ResolveTracks(guildID, input, source, parser string) ([]sources.TrackInfo, error) {
	s.mu.Lock()
	if s.resolver == nil {
		s.resolver = resolve.New()
	}
	r := s.resolver
	s.mu.Unlock()
	return r.Resolve(input, source, parser)
}

// SetGuildMusicNotifyChannel records the text channel id for guild (slash command channel) so async
// playback failure can post a public embed when the status message is not registered yet.
func (s *Service) SetGuildMusicNotifyChannel(guildID, channelID string) {
	if guildID == "" || channelID == "" {
		return
	}
	s.guildMusicStatusMu.Lock()
	if s.guildMusicNotifyChannel == nil {
		s.guildMusicNotifyChannel = make(map[string]string)
	}
	s.guildMusicNotifyChannel[guildID] = channelID
	s.guildMusicStatusMu.Unlock()
}

// UpdatePlaybackStatus creates or edits the guild's music status message.
func (s *Service) UpdatePlaybackStatus(session *discordgo.Session, i *discordgo.InteractionCreate, guildID string, embed *discordgo.MessageEmbed) error {
	if i != nil && i.ChannelID != "" {
		s.guildMusicStatusMu.Lock()
		if s.guildMusicNotifyChannel == nil {
			s.guildMusicNotifyChannel = make(map[string]string)
		}
		s.guildMusicNotifyChannel[guildID] = i.ChannelID
		s.guildMusicStatusMu.Unlock()
	}

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

// InvalidateAllSinks disconnects and forgets current voice connections for all guilds,
// without stopping players or clearing queues. Intended for session restarts.
func (s *Service) InvalidateAllSinks() {
	s.mu.RLock()
	providers := make([]*sink.DiscordSinkProvider, 0, len(s.sinkProviders))
	for _, p := range s.sinkProviders {
		providers = append(providers, p)
	}
	s.mu.RUnlock()

	for _, p := range providers {
		if p == nil {
			continue
		}
		p.InvalidateSink()
	}
}
