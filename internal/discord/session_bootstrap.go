package discord

import (
	"context"
	"errors"

	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/discord/reply"
	"github.com/keshon/melodix/internal/discord/voice"
	"github.com/keshon/melodix/internal/discord/voice/sink"
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
	// survive reconnects. It is handed two functions rather than a session:
	// one that reaches the current connection through the lock that guards it,
	// and one that builds a guild's audio path. Between them they are the
	// service's entire contact with the library underneath.
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

// configureIntents takes the session rather than reading b.dg, which the
// caller already holds: b.dg is guarded by b.mu and reading it unlocked here
// only worked because this happens to run on the goroutine that wrote it.
func (b *Bot) configureIntents(dg *discordgo.Session) {
	dg.Identify.Intents = discordgo.IntentsAll
}

// IsSessionUnhealthyError reports whether an error means we should fast-restart
// the session.
func IsSessionUnhealthyError(err error) bool {
	return errors.Is(err, ErrSessionUnhealthy)
}

// sessionAPI reaches the current session through the lock that guards it, in
// the neutral shape the voice service speaks. A nil return means there is no
// session right now, which is a normal state between restarts.
func (b *Bot) sessionAPI() cmdadapter.BotAPI {
	b.mu.RLock()
	dg := b.dg
	b.mu.RUnlock()
	if dg == nil {
		return nil
	}
	return reply.NewSessionAPI(dg)
}

// newSinkProvider builds the audio path for one guild. This is the discordgo
// one; VOICE_BACKEND chooses which of these the service is given.
func (b *Bot) newSinkProvider(guildID string) musicsink.Provider {
	delay := time.Duration(b.cfg.VoiceReadyDelayMs) * time.Millisecond
	return sink.NewDiscordSinkProvider(b.session, guildID, delay, b.log)
}

// session is the raw session the discordgo sink provider needs; it joins voice
// channels through the library directly rather than through the neutral API.
func (b *Bot) session() *discordgo.Session {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.dg
}
