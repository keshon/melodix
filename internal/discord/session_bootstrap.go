package discord

import (
	"context"
	"errors"
	"time"

	disgovoice "github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/snowflake/v2"

	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/discord/cmdqueue"
	"github.com/keshon/melodix/internal/discord/voice"
	"github.com/keshon/melodix/internal/discord/voice/voicesink"
	"github.com/keshon/melodix/internal/storage"
	"github.com/keshon/melodix/pkg/music/parsers/ffmpeg"
	"github.com/keshon/melodix/pkg/music/parsers/kkdai"
	"github.com/keshon/melodix/pkg/music/parsers/ytdlp"
	"github.com/keshon/melodix/pkg/music/parsers/ytnative"
	"github.com/keshon/melodix/pkg/music/sink"
	"github.com/keshon/melodix/pkg/music/soundcloudapi"
	"github.com/rs/zerolog"
)

// NewBot creates a Bot. Register any bot-dependent commands before calling Run.
func NewBot(cfg *config.Config, storage *storage.Storage, log zerolog.Logger) *Bot {
	b := &Bot{
		cfg:     cfg,
		storage: storage,
		log:     log,
	}
	// Voice service must outlive a single Discord session so playback/queues
	// survive reconnects. It is handed two functions rather than a connection:
	// one that reaches the live one, and one that builds a guild's audio path.
	// Between them they are the service's entire contact with the library
	// underneath.
	b.voice = voice.NewVoiceService(b.sessionAPI, b.newSinkProvider, cfg, storage, log)
	b.commands = cmdqueue.New(log)
	b.setSessionContext(context.Background())
	b.setGuard(disabledGuard)
	kkdai.SetLogger(log)
	ffmpeg.SetLogger(log)
	soundcloudapi.SetLogger(log)
	ytnative.SetLogger(log)
	ytdlp.SetLogger(log)
	return b
}

// drainCommands stops accepting commands and waits for the ones already
// running. Call on shutdown, before stopping players.
func (b *Bot) drainCommands() {
	if b.commands == nil {
		return
	}
	// closeWithin already bounds this phase and reports what it took, so the
	// wait here is unbounded on purpose: two budgets for one step means the
	// log names a timeout that is not the one that fired.
	b.commands.Close(context.Background())
	b.log.Info().Msg("commands_drained")
}

// stopAllPlayers stops playback and disconnects voice for all guilds. Call on
// shutdown.
func (b *Bot) stopAllPlayers() {
	if b.voice != nil {
		b.voice.StopAllPlayers()
	}
	b.log.Info().Msg("players_all_stopped")
}

// IsSessionUnhealthyError reports whether an error means we should fast-restart
// the session.
func IsSessionUnhealthyError(err error) bool {
	return errors.Is(err, ErrSessionUnhealthy)
}

// sessionAPI is the neutral surface over the live connection, or nil when
// there is none -- which is a normal state between restarts.
func (b *Bot) sessionAPI() cmdadapter.BotAPI {
	c := b.currentConn()
	if c == nil {
		return nil
	}
	return c.API()
}

// newSinkProvider builds the audio path for one guild. The provider outlives
// every session, like the player that will hold it, and reaches the live one
// through voiceResources on each acquisition.
func (b *Bot) newSinkProvider(guildID string) sink.Provider {
	gid, err := snowflake.Parse(guildID)
	if err != nil {
		b.log.Error().Str("guild_id", guildID).Err(err).Msg("voice_guild_id_invalid")
		return deadSinkProvider{}
	}
	delay := time.Duration(b.cfg.VoiceReadyDelayMs) * time.Millisecond
	return voicesink.NewProvider(b.voiceResources, gid, delay, b.log)
}

// voiceResources reaches the live session's voice manager and DAVE registry;
// ok is false between sessions, which is a normal state rather than a failure.
func (b *Bot) voiceResources() (disgovoice.Manager, *voicesink.DaveRegistry, bool) {
	c := b.currentConn()
	if c == nil {
		return nil, nil, false
	}
	manager, dave := c.VoiceResources()
	return manager, dave, manager != nil
}
