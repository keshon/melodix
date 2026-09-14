package discord

import (
	"context"
	"errors"

	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/discord/voice"
	"github.com/keshon/melodix/internal/storage"
	"github.com/keshon/melodix/pkg/music/parsers/ffmpeg"
	"github.com/keshon/melodix/pkg/music/parsers/kkdai"
	"github.com/keshon/melodix/pkg/music/parsers/ytdlp"
	"github.com/keshon/melodix/pkg/music/parsers/ytnative"
	musicsink "github.com/keshon/melodix/pkg/music/sink"
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
	b.sessionCtx.Store(&sessionCtxHolder{ctx: context.Background()})
	b.cmdGuard.Store(&cmdGuardHolder{g: disabledGuard})
	kkdai.SetLogger(log)
	ffmpeg.SetLogger(log)
	soundcloudapi.SetLogger(log)
	ytnative.SetLogger(log)
	ytdlp.SetLogger(log)
	return b
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

// newSinkProvider builds the audio path for one guild on the live connection.
// A guild that asks between sessions gets a provider that cannot join, which
// is the same answer it got from a nil session before.
func (b *Bot) newSinkProvider(guildID string) musicsink.Provider {
	c := b.currentConn()
	if c == nil {
		return deadSinkProvider{}
	}
	return c.NewSinkProvider(guildID)
}
