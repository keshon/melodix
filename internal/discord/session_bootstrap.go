package discord

import (
	"context"
	"errors"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/discord/voice"
	"github.com/keshon/melodix/internal/storage"
	"github.com/keshon/melodix/pkg/music/parsers/ffmpeg"
	"github.com/keshon/melodix/pkg/music/parsers/kkdai"
	"github.com/rs/zerolog"
)

// NewBot creates a Bot. Register any bot-dependent commands before calling Run.
func NewBot(cfg *config.Config, storage *storage.Storage, log zerolog.Logger) *Bot {
	b := &Bot{
		cfg:       cfg,
		storage:   storage,
		log:       log,
		slashCmds: make(map[string][]*discordgo.ApplicationCommand),
	}
	// Voice service must outlive a single Discord session so playback/queues survive reconnects.
	b.voice = voice.NewVoiceService(func() *discordgo.Session {
		b.mu.RLock()
		s := b.dg
		b.mu.RUnlock()
		return s
	}, cfg, storage, log)
	b.sessionCtx.Store(&sessionCtxHolder{ctx: context.Background()})
	b.cmdGuard.Store(&cmdGuardHolder{g: disabledGuard})
	kkdai.SetLogger(log)
	ffmpeg.SetLogger(log)
	return b
}

// stopAllPlayers stops playback and disconnects voice for all guilds. Call on shutdown.
func (b *Bot) stopAllPlayers() {
	if b.voice != nil {
		b.voice.StopAllPlayers()
	}
	b.log.Info().Msg("players_all_stopped")
}

func (b *Bot) configureIntents() {
	b.dg.Identify.Intents = discordgo.IntentsAll
}

// IsSessionUnhealthyError reports whether an error means we should fast-restart the session.
func IsSessionUnhealthyError(err error) bool {
	return errors.Is(err, ErrSessionUnhealthy)
}
