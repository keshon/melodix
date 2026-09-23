package voice

import (
	"errors"
	"fmt"
	"sync"

	"github.com/keshon/melodix/internal/discord/adapter"
	"github.com/keshon/melodix/internal/discord/reply"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/player"
	"github.com/rs/zerolog"
)

// APIGetter returns the current connection's neutral surface, or nil when
// there is no session. It is a function rather than a value so the service
// survives reconnects: a restart replaces the session, and everything here
// asks again rather than holding a handle that has gone stale.
type APIGetter func() adapter.BotAPI

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

// ErrAnnounceFailed marks a PlayNextAndAnnounce error that came after the
// track had started: playback is running, only the message about it is
// missing. A caller must not report it as a failure to play, which is what the
// user would otherwise be told over the music.
var ErrAnnounceFailed = errors.New("playback started, but announcing it failed")

type guildMusicStatus struct {
	ChannelID string
	MessageID string
}

// Service is the Discord side of music: where a guild's playback is shown and
// who is in which voice channel. The players themselves belong to
// music.Service; this one is plugged into it as the watcher and the failure
// report. It is pluggable: a bot without voice can omit it.
type Service struct {
	getAPI APIGetter
	log    zerolog.Logger

	guildMusicStatus map[string]guildMusicStatus
	// guildMusicNotifyChannel is the text channel of the last music slash (/play,
	// /next, …) for fallback "Playback failed" when no status message id is stored
	// yet or edit fails.
	guildMusicNotifyChannel map[string]string
	guildMusicStatusMu      sync.RWMutex
}

// NewVoiceService creates a voice service. getAPI reaches the current
// connection, which is the service's entire contact with whichever library is
// running.
func NewVoiceService(getAPI APIGetter, log zerolog.Logger) *Service {
	return &Service{
		getAPI:                  getAPI,
		log:                     log,
		guildMusicStatus:        make(map[string]guildMusicStatus),
		guildMusicNotifyChannel: make(map[string]string),
	}
}

// NotifyPlaybackFailed is music.Hooks.OnFailed: it shows a failure that
// happened after the track had started.
func (s *Service) NotifyPlaybackFailed(guildID string, track parsers.Track, err error) {
	api := s.getAPI()
	if api == nil {
		return
	}
	detail := reply.ClampEmbedText(err.Error())
	var desc string
	if track.Title != "" && track.URL != "" {
		desc = fmt.Sprintf("%s\n\n[%s](%s)", detail, track.Title, track.URL)
	} else if track.Title != "" {
		desc = fmt.Sprintf("%s\n\n%s", detail, track.Title)
	} else {
		desc = detail
	}
	s.deliverPlaybackFailureEmbed(api, guildID, &adapter.Embed{
		Title:       "Playback failed",
		Description: desc,
		Color:       reply.EmbedColor,
	})
}

// deliverPlaybackFailureEmbed edits the stored "now playing" message when
// possible; otherwise sends a public embed to the last known slash channel (see
// SetGuildMusicNotifyChannel / UpdatePlaybackStatus).
func (s *Service) deliverPlaybackFailureEmbed(api adapter.BotAPI, guildID string, embed *adapter.Embed) {
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

// WatchPlayerStatus is music.Hooks.Watch: the single long-lived consumer of the
// player's status channel (one per guild, for the player's lifetime).
// Interaction-driven starts are rendered by PlayNextAndAnnounce, which detaches
// the old status message first so this watcher cannot write the new track into
// it; what is left here is the asynchronous half -- auto-advance, parser
// corrections, queue end.
func (s *Service) WatchPlayerStatus(guildID string, p *player.Player) {
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
			track, ok := p.CurrentTrack()
			if !ok {
				s.log.Warn().Str("guild_id", guildID).Msg("now_playing_render_skipped_no_track")
				continue
			}
			// UpdatePlaybackStatus is a silent no-op when no status message
			// is registered, so check first — otherwise this traces a render
			// that never happened.
			registered := s.hasStatusMessage(guildID)
			if err := s.UpdatePlaybackStatus(guildID, reply.NowPlayingEmbed(&track)); err != nil {
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
			if err := s.UpdatePlaybackStatus(guildID, reply.PlaybackFinishedEmbed()); err != nil {
				s.log.Warn().Str("guild_id", guildID).Err(err).Msg("guild_status_update_failed")
			}
		case player.StatusAdded, player.StatusError, player.StatusPaused, player.StatusResumed:
			// Rendered elsewhere or never emitted: the command that queued
			// tracks answers with them, a playback failure is posted by
			// onPlaybackFailed, and the player supports neither pause nor
			// resume. Listed so a new status has to be placed here too.
		}
	}
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

// PlayNextAndAnnounce starts the guild's next queued track and makes the
// answer to the interaction the guild's status message. It is the only path by
// which a status message comes to exist, and the order inside it is the point.
//
// The old message is detached before PlayNext. The player announces Playing
// while PlayNext is still running, before the new message has been posted, and
// the watcher renders Playing into whatever is registered at that moment: while
// the start was split between this service and the commands, that was the
// previous status message, which got the new track written into it and then
// stayed on that track for good, above the message that went on being updated.
//
// skipped is the track /next is moving past, or nil. Its status message would
// otherwise go on saying that track is playing, so it is marked skipped.
//
// A component interaction's own answer is the message that carried the
// component -- for /search, the ephemeral chooser -- which only the person who
// clicked can see and which the channel endpoint cannot edit later. There the
// status message is posted publicly instead, and the chooser is answered with
// what happened to the pick.
func (s *Service) PlayNextAndAnnounce(to adapter.Interaction, p *player.Player, guildID, voiceChannelID string, added int, skipped *parsers.Track) error {
	if to == nil || p == nil {
		return nil
	}
	s.rememberNotifyChannel(guildID, to.ChannelID())

	old, hadOld := s.detachStatusMessage(guildID)
	if err := p.PlayNext(voiceChannelID); err != nil {
		// Nothing replaced it, so it is still the guild's status message: a
		// failure reported later still lands where people are looking.
		if hadOld {
			s.restoreStatusMessage(guildID, old)
		}
		return err
	}
	if hadOld && skipped != nil {
		if api := s.getAPI(); api != nil {
			if err := api.EditChannelEmbed(old.ChannelID, old.MessageID, reply.SkippedEmbed(skipped)); err != nil {
				s.log.Warn().Str("guild_id", guildID).Err(err).Msg("skipped_status_update_failed")
			}
		}
	}

	embed := reply.TracksAddedEmbed(added)
	if track, ok := p.CurrentTrack(); ok {
		embed = reply.NowPlayingEmbed(&track)
	}
	msg, err := s.postStatusMessage(to, embed, added)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrAnnounceFailed, err)
	}
	if msg.MessageID != "" {
		s.guildMusicStatusMu.Lock()
		s.guildMusicStatus[guildID] = msg
		s.guildMusicStatusMu.Unlock()
	}
	return nil
}

// postStatusMessage answers to with embed where that answer can serve as the
// status message, and posts it publicly where it cannot.
func (s *Service) postStatusMessage(to adapter.Interaction, embed *adapter.Embed, added int) (guildMusicStatus, error) {
	if !to.Component() {
		channelID, messageID, err := to.AnswerEmbedMessage(embed)
		return guildMusicStatus{ChannelID: channelID, MessageID: messageID}, err
	}

	// The chooser gets an answer either way: the pick is what its owner is
	// waiting to hear about.
	if _, _, err := to.AnswerEmbedMessage(reply.TracksAddedEmbed(added)); err != nil {
		s.log.Warn().Err(err).Msg("pick_answer_failed")
	}
	api := s.getAPI()
	if api == nil {
		return guildMusicStatus{}, nil
	}
	messageID, err := api.PostChannelEmbed(to.ChannelID(), embed)
	return guildMusicStatus{ChannelID: to.ChannelID(), MessageID: messageID}, err
}

func (s *Service) detachStatusMessage(guildID string) (guildMusicStatus, bool) {
	s.guildMusicStatusMu.Lock()
	defer s.guildMusicStatusMu.Unlock()
	msg, ok := s.guildMusicStatus[guildID]
	delete(s.guildMusicStatus, guildID)
	return msg, ok
}

// restoreStatusMessage puts a detached message back, unless something has
// registered a newer one in the meantime.
func (s *Service) restoreStatusMessage(guildID string, msg guildMusicStatus) {
	s.guildMusicStatusMu.Lock()
	defer s.guildMusicStatusMu.Unlock()
	if _, taken := s.guildMusicStatus[guildID]; !taken {
		s.guildMusicStatus[guildID] = msg
	}
}

// UpdatePlaybackStatus edits the guild's playback status message.
//
// This is the asynchronous half -- auto-advance, queue end -- where there is
// nobody to answer and the message must already exist. A guild with no status
// message registered is a no-op rather than an error: playback can start from
// a path that never announced one, and a missing message is not a failure.
func (s *Service) UpdatePlaybackStatus(guildID string, embed *adapter.Embed) error {
	s.guildMusicStatusMu.RLock()
	msg, ok := s.guildMusicStatus[guildID]
	s.guildMusicStatusMu.RUnlock()
	if !ok {
		return nil
	}

	api := s.getAPI()
	if api == nil {
		return nil
	}
	return api.EditChannelEmbed(msg.ChannelID, msg.MessageID, embed)
}

func (s *Service) rememberNotifyChannel(guildID, channelID string) {
	if channelID == "" {
		return
	}
	s.guildMusicStatusMu.Lock()
	if s.guildMusicNotifyChannel == nil {
		s.guildMusicNotifyChannel = make(map[string]string)
	}
	s.guildMusicNotifyChannel[guildID] = channelID
	s.guildMusicStatusMu.Unlock()
}
