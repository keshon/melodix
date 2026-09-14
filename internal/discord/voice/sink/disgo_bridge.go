package sink

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/bwmarrin/discordgo"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/snowflake/v2"
	"github.com/rs/zerolog"
	davesession "github.com/thomas-vilte/dave-go/session"
)

// DisgoVoice runs disgo's voice stack on top of the discordgo gateway, so the
// two voice implementations can be compared inside one running bot instead of
// across two builds. Everything above the audio path — commands, REST,
// interactions, state — stays on discordgo and does not know this exists.
//
// Nothing in disgo's voice package needs disgo's gateway. A Manager is given a
// StateUpdateFunc to send the op 4 voice state update with, and is fed the two
// events that come back through HandleVoiceStateUpdate and
// HandleVoiceServerUpdate. discordgo does all three: Session.VoiceStateUpdate
// sends op 4, and AddHandler delivers the events. The Conn filters both by
// guild and user itself, so they are forwarded unfiltered.
//
// DAVE comes from dave-go, which is pure Go. Do not swap it for
// disgoorg/godave's own session: that one links libdave over cgo, and
// .github/workflows/release.yml cross-compiles six targets with CGO_ENABLED=0
// from Linux runners, which a C++ dependency ends. Measured on this tree, not
// assumed: it builds for windows/arm64 and darwin/arm64 with cgo off, and
// `go list -deps` pulls in godave's interface package alone, never
// golibdave.
type DisgoVoice struct {
	log zerolog.Logger

	mu      sync.Mutex
	session *discordgo.Session
	manager voice.Manager

	// dave holds each guild connection's DAVE session, which is the only way
	// to ask whether encryption has come up. disgo v0.19.6 exposes no route
	// to it from a Conn — Conn.DAVE() exists only on the unreleased branch —
	// so it is caught from dave-go's session hook, which runs inside
	// CreateConn while mu is held. Every CreateConn has to come through this
	// type for that to hold; don't call the manager directly.
	dave map[snowflake.ID]*davesession.Session
	// pending carries the hook's session out of one CreateConn call.
	pending *davesession.Session
}

// NewDisgoVoice creates the bridge. The manager itself is built on first use,
// because it needs the bot's own user ID and that is only known once the
// gateway has delivered READY.
func NewDisgoVoice(log zerolog.Logger) *DisgoVoice {
	return &DisgoVoice{
		log:  log.With().Str("component", "disgo_voice").Logger(),
		dave: make(map[snowflake.ID]*davesession.Session),
	}
}

// Conn returns the guild's voice connection and the DAVE session behind it,
// creating both if needed. The session is nil only if dave-go's hook did not
// fire, which would mean disgo changed when it builds one.
func (d *DisgoVoice) Conn(dg *discordgo.Session, guildID snowflake.ID) (voice.Conn, *davesession.Session, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.ensureManagerLocked(dg); err != nil {
		return nil, nil, err
	}

	d.pending = nil
	conn := d.manager.CreateConn(guildID)
	if d.pending != nil {
		d.dave[guildID] = d.pending
		d.pending = nil
	}
	return conn, d.dave[guildID], nil
}

// Remove closes the guild's connection and its DAVE session. dave-go arms
// recovery watchdogs that keep re-arming invalidations on a channel the bot
// has left until they expire, so the Close matters even though it is bounded.
func (d *DisgoVoice) Remove(ctx context.Context, guildID snowflake.ID) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.manager == nil {
		return
	}
	if conn := d.manager.GetConn(guildID); conn != nil {
		conn.Close(ctx)
	}
	d.manager.RemoveConn(guildID)

	if s := d.dave[guildID]; s != nil {
		if err := s.Close(); err != nil {
			d.log.Warn().Str("guild_id", guildID.String()).Err(err).Msg("dave_session_close_failed")
		}
		delete(d.dave, guildID)
	}
}

func (d *DisgoVoice) ensureManagerLocked(dg *discordgo.Session) error {
	if d.manager != nil && d.session == dg {
		return nil
	}
	if dg.State == nil || dg.State.User == nil {
		return errors.New("disgo voice: session has no user yet")
	}
	userID, err := snowflake.Parse(dg.State.User.ID)
	if err != nil {
		return fmt.Errorf("disgo voice: parsing bot user id: %w", err)
	}

	logger := slog.New(slog.NewTextHandler(disgoLogWriter{log: d.log}, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	manager := voice.NewManager(
		func(_ context.Context, guildID snowflake.ID, channelID *snowflake.ID, selfMute, selfDeaf bool) error {
			channel := ""
			if channelID != nil {
				channel = channelID.String()
			}
			return dg.VoiceStateUpdate(guildID.String(), channel, selfMute, selfDeaf)
		},
		userID,
		voice.WithLogger(logger),
		voice.WithDaveSessionLogger(logger),
		voice.WithDaveSessionCreateFunc(davesession.CreateFunc(
			davesession.WithSessionHook(func(s *davesession.Session) { d.pending = s }),
		)),
	)

	dg.AddHandler(func(_ *discordgo.Session, e *discordgo.VoiceServerUpdate) {
		guildID, err := snowflake.Parse(e.GuildID)
		if err != nil {
			return
		}
		manager.HandleVoiceServerUpdate(gateway.EventVoiceServerUpdate{
			Token:    e.Token,
			GuildID:  guildID,
			Endpoint: e.Endpoint,
		})
	})
	dg.AddHandler(func(_ *discordgo.Session, e *discordgo.VoiceStateUpdate) {
		update, ok := voiceStateUpdate(e)
		if !ok {
			return
		}
		manager.HandleVoiceStateUpdate(update)
	})

	d.manager = manager
	d.session = dg
	d.log.Info().Str("user_id", dg.State.User.ID).Msg("disgo_voice_manager_built")
	return nil
}

// voiceStateUpdate translates the discordgo event into disgo's. Only the four
// fields a Conn reads are carried across: the rest of the voice state is not
// consulted, and inventing values for it would only invite someone to believe
// them.
func voiceStateUpdate(e *discordgo.VoiceStateUpdate) (gateway.EventVoiceStateUpdate, bool) {
	var update gateway.EventVoiceStateUpdate
	if e == nil || e.VoiceState == nil {
		return update, false
	}
	guildID, err := snowflake.Parse(e.GuildID)
	if err != nil {
		return update, false
	}
	userID, err := snowflake.Parse(e.UserID)
	if err != nil {
		return update, false
	}

	update.GuildID = guildID
	update.UserID = userID
	update.SessionID = e.SessionID
	if e.ChannelID != "" {
		channelID, err := snowflake.Parse(e.ChannelID)
		if err != nil {
			return update, false
		}
		update.ChannelID = &channelID
	}
	return update, true
}

// disgoLogWriter turns disgo's slog output into one zerolog event per line,
// the same shape attachDiscordgoLogger gives the discordgo fork: the library's
// own text is a field rather than the event name, so every line stays
// greppable under one name.
type disgoLogWriter struct {
	log zerolog.Logger
}

func (w disgoLogWriter) Write(p []byte) (int, error) {
	w.log.Info().Str("raw", strings.TrimRight(string(p), "\n")).Msg("disgo_log")
	return len(p), nil
}
