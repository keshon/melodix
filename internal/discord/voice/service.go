package voice

import (
	"fmt"
	"sync"
	"time"

	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/discord/reply"
	"github.com/keshon/melodix/internal/playbackerr"
	"github.com/keshon/melodix/internal/storage"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/player"
	"github.com/keshon/melodix/pkg/music/resolve"
	musicsink "github.com/keshon/melodix/pkg/music/sink"
	"github.com/keshon/melodix/pkg/music/sources"
	"github.com/rs/zerolog"
)

// APIGetter returns the current connection's neutral surface, or nil when
// there is no session. It is a function rather than a value so the service
// survives reconnects: a restart replaces the session, and everything here
// asks again rather than holding a handle that has gone stale.
type APIGetter func() cmdadapter.BotAPI

// SinkProviderFactory builds the audio path for one guild. It is supplied by
// whatever is holding the connection -- the service itself does not know which
// library carries the packets, and does not need to.
type SinkProviderFactory func(guildID string) musicsink.Provider

// UserVoiceState is where a user is connected, in the shape a caller needs to
// join them: the channel, and who was asked about.
type UserVoiceState struct {
	ChannelID string
	UserID    string
}

// FindUserVoiceState reports the voice channel a user is in.
//
// It reads the session through getSession like every other method here, which
// is the point: the Bot used to answer this one itself off its own session
// field, without the lock that guards it, while a session restart wrote that
// field from another goroutine.
func (s *Service) FindUserVoiceState(guildID, userID string) (*UserVoiceState, error) {
	api := s.getAPI()
	if api == nil {
		return nil, fmt.Errorf("no Discord session")
	}
	channelID, err := api.UserVoiceChannel(guildID, userID)
	if err != nil {
		return nil, err
	}
	return &UserVoiceState{ChannelID: channelID, UserID: userID}, nil
}

type guildMusicStatus struct {
	ChannelID string
	MessageID string
}

// Service provides voice/music for a Discord bot: players, sink providers,
// resolver, and guild music status. It is pluggable: a bot without voice can
// omit it.
type Service struct {
	getAPI          APIGetter
	newSinkProvider SinkProviderFactory
	cfg             *config.Config
	store           *storage.Storage
	log             zerolog.Logger
	mu              sync.RWMutex
	players         map[string]*player.Player
	sinkProviders   map[string]musicsink.Provider
	resolver        *resolve.Resolver

	guildMusicStatus map[string]guildMusicStatus
	// guildMusicNotifyChannel is the text channel of the last music slash (/play,
	// /next, …) for fallback "Playback failed" when no status message id is stored
	// yet or edit fails.
	guildMusicNotifyChannel map[string]string
	guildMusicStatusMu      sync.RWMutex
}

// NewVoiceService creates a voice service. getAPI reaches the current
// connection and newSinkProvider builds a guild's audio path; between them
// they are the entire contact with whichever library is running.
func NewVoiceService(getAPI APIGetter, newSinkProvider SinkProviderFactory, cfg *config.Config, store *storage.Storage, log zerolog.Logger) *Service {
	return &Service{
		getAPI:                  getAPI,
		newSinkProvider:         newSinkProvider,
		cfg:                     cfg,
		store:                   store,
		log:                     log,
		players:                 make(map[string]*player.Player),
		sinkProviders:           make(map[string]musicsink.Provider),
		guildMusicStatus:        make(map[string]guildMusicStatus),
		guildMusicNotifyChannel: make(map[string]string),
	}
}

type playbackRecorder struct {
	store *storage.Storage
	log   zerolog.Logger
}

func (r playbackRecorder) Record(guildID string, playedAt time.Time, track parsers.Track) {
	if r.store == nil {
		return
	}
	if _, err := r.store.AppendMusicPlayback(guildID, track, playedAt); err != nil {
		r.log.Warn().Str("guild_id", guildID).Err(err).Msg("playback_history_append_failed")
	}
}

// notifyPlaybackFailed is wired as player.Options.OnPlaybackFailed at player construction.
func (s *Service) notifyPlaybackFailed(guildID string, track parsers.Track, err error) {
	api := s.getAPI()
	if api == nil {
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
	s.deliverPlaybackFailureEmbed(api, guildID, &cmdadapter.Embed{
		Title:       "Playback failed",
		Description: desc,
		Color:       reply.EmbedColor,
	})
}

// deliverPlaybackFailureEmbed edits the stored "now playing" message when
// possible; otherwise sends a public embed to the last known slash channel (see
// SetGuildMusicNotifyChannel / UpdatePlaybackStatus).
func (s *Service) deliverPlaybackFailureEmbed(api cmdadapter.BotAPI, guildID string, embed *cmdadapter.Embed) {
	s.guildMusicStatusMu.RLock()
	msg, hasMsg := s.guildMusicStatus[guildID]
	notifyCh := s.guildMusicNotifyChannel[guildID]
	s.guildMusicStatusMu.RUnlock()

	if hasMsg && msg.ChannelID != "" && msg.MessageID != "" {
		if err := api.EditChannelEmbed(msg.ChannelID, msg.MessageID, embed); err != nil {
			s.log.Warn().Str("guild_id", guildID).Err(err).Msg("playback_failed_embed_edit_failed")
			if notifyCh != "" {
				if err2 := api.SendChannelEmbed(notifyCh, embed); err2 != nil {
					s.log.Warn().Str("guild_id", guildID).Str("channel_id", notifyCh).Err(err2).Msg("playback_failed_fallback_send_failed")
				} else {
					s.log.Info().Str("guild_id", guildID).Str("channel_id", notifyCh).Msg("playback_failed_sent_after_edit_failed")
				}
			}
		}
		return
	}

	if notifyCh != "" {
		if err := api.SendChannelEmbed(notifyCh, embed); err != nil {
			s.log.Warn().Str("guild_id", guildID).Str("channel_id", notifyCh).Err(err).Msg("playback_failed_channel_send_failed")
		} else {
			s.log.Info().Str("guild_id", guildID).Str("channel_id", notifyCh).Msg("playback_failed_sent_public_fallback")
		}
		return
	}

	s.log.Warn().Str("guild_id", guildID).Msg("playback_failed_no_ui_target")
}

// GetOrCreatePlayer returns an existing player for the guild or creates a new
// one.
func (s *Service) GetOrCreatePlayer(guildID string) *player.Player {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sinkProviders == nil {
		s.sinkProviders = make(map[string]musicsink.Provider)
	}
	if p, ok := s.players[guildID]; ok {
		return p
	}
	if s.resolver == nil {
		s.resolver = resolve.New()
	}
	provider, ok := s.sinkProviders[guildID]
	if !ok {
		provider = s.newSinkProvider(guildID)
		s.sinkProviders[guildID] = provider
	}
	recoveryMode, ok := player.ParseTransportRecoveryMode(s.cfg.PlayerTransportRecoveryMode)
	if !ok {
		s.log.Warn().Str("value", s.cfg.PlayerTransportRecoveryMode).Msg("unknown_transport_recovery_mode_using_hard")
	}
	p := player.NewWithOptions(provider, s.resolver, player.Options{
		Logger:                s.log,
		TransportRecoveryMode: recoveryMode,
		TransportSoftAttempts: s.cfg.PlayerTransportSoftAttempts,
		OnPlaybackFailed:      s.notifyPlaybackFailed,
	})
	p.SetGuildID(guildID)
	if s.store != nil {
		p.SetRecorder(playbackRecorder{store: s.store, log: s.log})
	}
	s.players[guildID] = p
	go s.watchPlayerStatus(guildID, p)
	return p
}

// watchPlayerStatus is the single long-lived consumer of the player's status
// channel (one per guild, for the player's lifetime). Slash handlers render
// interaction-driven updates synchronously; this watcher covers async
// transitions only: auto-advance to the next track and natural queue end. On an
// interaction-driven start both paths render the same "Now Playing" embed — the
// duplicate edit is invisible to users.
func (s *Service) watchPlayerStatus(guildID string, p *player.Player) {
	for status := range p.PlayerStatus {
		if s.getAPI() == nil {
			// Silence here used to make a stale embed undiagnosable: the
			// correction is computed and logged upstream, then nothing renders
			// and the log says nothing about why.
			s.log.Warn().Str("guild_id", guildID).Str("status", string(status)).
				Msg("status_render_skipped_no_session")
			continue
		}
		switch status {
		case player.StatusPlaying:
			track := p.CurrentTrack()
			if track == nil {
				s.log.Warn().Str("guild_id", guildID).Msg("now_playing_render_skipped_no_track")
				continue
			}
			// UpdatePlaybackStatus is a silent no-op when no status message is
			// registered and no interaction is available to create one, so check
			// first — otherwise this traces a render that never happened.
			registered := s.hasStatusMessage(guildID)
			if err := s.UpdatePlaybackStatus(nil, guildID, reply.NowPlayingEmbed(track)); err != nil {
				s.log.Warn().Str("guild_id", guildID).Err(err).Msg("guild_status_update_failed")
				continue
			}
			if !registered {
				// The slash handler posts the first embed and registers it; this
				// status arrived before that happened. Info rather than Debug:
				// this is the reason a Now Playing embed can name a parser that
				// has since been replaced, and it is the line a stale-chip report
				// needs to be answerable.
				s.log.Info().Str("guild_id", guildID).Str("parser", track.CurrentParser).
					Msg("now_playing_render_skipped")
				continue
			}
			// Traces the embed itself, so a chip that stayed stale can be told apart
			// from a correction that was computed but never rendered.
			s.log.Info().
				Str("guild_id", guildID).
				Str("parser", track.CurrentParser).
				Msg("now_playing_rendered")
		case player.StatusStopped:
			// A transient Stopped fires between tracks; only render the final one.
			if p.IsPlaying() || len(p.Queue()) > 0 {
				continue
			}
			if err := s.UpdatePlaybackStatus(nil, guildID, reply.PlaybackFinishedEmbed()); err != nil {
				s.log.Warn().Str("guild_id", guildID).Err(err).Msg("guild_status_update_failed")
			}
		}
	}
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

// SetGuildMusicNotifyChannel records the text channel id for guild (slash
// command channel) so async playback failure can post a public embed when the
// status message is not registered yet.
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

// hasStatusMessage reports whether a status message is registered for the
// guild, i.e. whether an interaction-less UpdatePlaybackStatus would actually
// render.
func (s *Service) hasStatusMessage(guildID string) bool {
	s.guildMusicStatusMu.RLock()
	defer s.guildMusicStatusMu.RUnlock()
	_, ok := s.guildMusicStatus[guildID]
	return ok
}

// UpdatePlaybackStatus creates or edits the guild's music status message.
//
// The interaction is optional and is only ever used to create the message: the
// status is edited for as long as the track plays, which outlives the
// interaction token, so editing goes through the session instead. A nil
// interaction is the asynchronous path -- auto-advance, queue end -- where
// there is nobody to reply to and the message must already exist.
func (s *Service) UpdatePlaybackStatus(from cmdadapter.Interaction, guildID string, embed *cmdadapter.Embed) error {
	if from != nil && from.ChannelID() != "" {
		s.guildMusicStatusMu.Lock()
		if s.guildMusicNotifyChannel == nil {
			s.guildMusicNotifyChannel = make(map[string]string)
		}
		s.guildMusicNotifyChannel[guildID] = from.ChannelID()
		s.guildMusicStatusMu.Unlock()
	}

	s.guildMusicStatusMu.RLock()
	msg, ok := s.guildMusicStatus[guildID]
	s.guildMusicStatusMu.RUnlock()

	if ok {
		api := s.getAPI()
		if api == nil {
			return nil
		}
		return api.EditChannelEmbed(msg.ChannelID, msg.MessageID, embed)
	}

	if from == nil {
		return nil
	}

	channelID, messageID, err := from.AnswerEmbedMessage(embed)
	if err != nil {
		return err
	}
	if messageID == "" {
		return nil
	}

	s.guildMusicStatusMu.Lock()
	s.guildMusicStatus[guildID] = guildMusicStatus{ChannelID: channelID, MessageID: messageID}
	s.guildMusicStatusMu.Unlock()
	return nil
}

// StopAllPlayers stops playback and disconnects voice for all guilds. Call on
// shutdown.
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

// InvalidateAllSinks disconnects and forgets current voice connections for all
// guilds, without stopping players or clearing queues. Intended for session
// restarts.
func (s *Service) InvalidateAllSinks() {
	s.mu.RLock()
	providers := make([]musicsink.Provider, 0, len(s.sinkProviders))
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
