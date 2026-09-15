package discord

import (
	"context"
	"slices"
	"sync/atomic"
	"time"

	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/discord/cmdqueue"
	"github.com/keshon/melodix/internal/discord/execguard"
	"github.com/keshon/melodix/internal/discord/voice"
	"github.com/keshon/melodix/internal/storage"
	"github.com/rs/zerolog"
)

// Bot is the Discord bot. Lifecycle is managed by Run/run; handlers are wired
// in run.
type Bot struct {
	storage *storage.Storage
	cfg     *config.Config
	voice   *voice.Service
	log     zerolog.Logger

	// commands runs command bodies off the gateway read goroutine. Process
	// lifetime, like the voice service: a command outliving the session it
	// arrived on is a command that cannot answer, not a command to abandon
	// halfway through whatever it was doing to the player.
	commands *cmdqueue.Queue

	sessionCtx atomic.Value // *sessionCtxHolder
	cmdGuard   atomic.Value // *cmdGuardHolder
	conn       atomic.Value // *connHolder
}

type sessionCtxHolder struct {
	ctx context.Context
}

type cmdGuardHolder struct {
	g *execguard.Guard
}

var disabledGuard = execguard.New(0)

// slotWaitBudget bounds how long a command waits for a free slot before it
// gives up and says the bot is busy.
//
// It is short because it is measured against Discord's deadline, not ours: an
// interaction must be acknowledged within three seconds of being created, and
// a command that has not started by then cannot answer at all. Better to
// refuse in a way the user can read than to succeed into a token that has
// already expired.
const slotWaitBudget = 2 * time.Second

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

// commandContext is the session's context, cancelled when the session ends.
// It carries no deadline: nothing downstream of here takes a context, so one
// would only be a claim.
func (b *Bot) commandContext() (context.Context, context.CancelFunc) {
	return context.WithCancel(b.baseSessionContext())
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
