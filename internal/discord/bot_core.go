package discord

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/discord/commandlogger"
	"github.com/keshon/melodix/internal/discord/commandsync"
	"github.com/keshon/melodix/internal/discord/execguard"
	"github.com/keshon/melodix/internal/discord/systemevents"
	"github.com/keshon/melodix/internal/discord/voice"
	"github.com/keshon/melodix/internal/storage"
	"github.com/rs/zerolog"
)

// Bot is the Discord bot. Lifecycle is managed by Run/run; handlers are wired in run.
type Bot struct {
	dg        *discordgo.Session
	storage   *storage.Storage
	slashCmds map[string][]*discordgo.ApplicationCommand
	cfg       *config.Config
	mu        sync.RWMutex
	voice     *voice.Service
	log       zerolog.Logger

	cmdManager *commandsync.Syncer
	cmdLogger  *commandlogger.Logger

	systemBus *systemevents.Bus

	sessionCtx atomic.Value // *sessionCtxHolder
	cmdGuard   atomic.Value // *cmdGuardHolder

	// once ensures one-time background services (purge, shortlink) are not
	// re-launched on subsequent reconnects.
	once sync.Once
}

type sessionCtxHolder struct {
	ctx context.Context
}

type cmdGuardHolder struct {
	g *execguard.Guard
}

var disabledGuard = execguard.New(0, 0)

func (b *Bot) baseSessionContext() context.Context {
	if v := b.sessionCtx.Load(); v != nil {
		if holder, ok := v.(*sessionCtxHolder); ok && holder != nil && holder.ctx != nil {
			return holder.ctx
		}
	}
	return context.Background()
}

func (b *Bot) guard() *execguard.Guard {
	if v := b.cmdGuard.Load(); v != nil {
		if holder, ok := v.(*cmdGuardHolder); ok && holder != nil && holder.g != nil {
			return holder.g
		}
	}
	return disabledGuard
}

func (b *Bot) commandContext() (context.Context, context.CancelFunc) {
	base := b.baseSessionContext()
	return b.guard().Context(base)
}

func (b *Bot) acquireCommandSlot(ctx context.Context) error {
	return b.guard().Acquire(ctx)
}

func (b *Bot) releaseCommandSlot() {
	b.guard().Release()
}

func (b *Bot) isGuildBlacklisted(guildID string) bool {
	return slices.Contains(b.cfg.DiscordGuildBlacklist, guildID)
}
