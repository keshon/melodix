package sink

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/bwmarrin/discordgo"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/snowflake/v2"
	"github.com/rs/zerolog"
)

// DisgoVoice runs disgo's voice stack on top of the discordgo gateway, so the
// two audio paths can be compared inside one running bot instead of across two
// builds. Everything above the audio path -- commands, REST, interactions,
// state -- stays on discordgo and does not know this exists.
//
// Nothing in disgo's voice package needs disgo's gateway. A Manager is given a
// StateUpdateFunc to send the op 4 voice state update with, and is fed the two
// events that come back through HandleVoiceStateUpdate and
// HandleVoiceServerUpdate. discordgo does all three: Session.VoiceStateUpdate
// sends op 4, and AddHandler delivers the events. The Conn filters both by
// guild and user itself, so they are forwarded unfiltered.
//
// The mirror image -- the fork's voice on a disgo gateway -- is not built.
// Nothing has ever needed it, and a disgo gateway already has a voice manager
// of its own.
type DisgoVoice struct {
	log  zerolog.Logger
	dave *DaveRegistry

	mu      sync.Mutex
	session *discordgo.Session
	manager voice.Manager
}

// NewDisgoVoice creates the bridge. The manager itself is built on first use,
// because it needs the bot's own user ID and that is only known once the
// gateway has delivered READY.
func NewDisgoVoice(log zerolog.Logger) *DisgoVoice {
	return &DisgoVoice{
		log:  log.With().Str("component", "disgo_voice").Logger(),
		dave: NewDaveRegistry(),
	}
}

// Manager returns the voice manager, building it against this session if it
// has not been built or the session has been replaced.
func (d *DisgoVoice) Manager(dg *discordgo.Session) (voice.Manager, *DaveRegistry, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.manager != nil && d.session == dg {
		return d.manager, d.dave, nil
	}
	if dg.State == nil || dg.State.User == nil {
		return nil, nil, errors.New("disgo voice: session has no user yet")
	}
	userID, err := snowflake.Parse(dg.State.User.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("disgo voice: parsing bot user id: %w", err)
	}

	logger := slog.New(slog.NewTextHandler(bridgeLogWriter{log: d.log}, &slog.HandlerOptions{
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
		d.dave.ManagerOptions(logger)...,
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
	return manager, d.dave, nil
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

// bridgeLogWriter turns disgo's slog output into one zerolog event per line,
// the same shape attachDiscordgoLogger gives the fork: the library's own text
// is a field rather than the event name, so every line stays greppable under
// one name.
type bridgeLogWriter struct {
	log zerolog.Logger
}

func (w bridgeLogWriter) Write(p []byte) (int, error) {
	w.log.Info().Str("raw", trimNewline(string(p))).Msg("disgo_log")
	return len(p), nil
}

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
