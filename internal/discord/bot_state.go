package discord

import (
	"context"
	"slices"
	"sync/atomic"
	"time"

	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/discord/cmdqueue"
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

	// Replaced wholesale when a session opens and cleared when one closes, so
	// a reader gets a live one or the fallback, never a half-torn one.
	//
	// atomic.Pointer rather than atomic.Value: Value stores an interface and
	// panics if the concrete type it is given ever changes, which is why each
	// of these used to be a one-field struct wrapping what it actually held --
	// a box whose only job was to be a single type. There was even a test that
	// storing the same type twice does not panic, which is a test of the
	// standard library.
	sessionCtx atomic.Pointer[context.Context]
	conn       atomic.Pointer[conn]
}

// slotWaitBudget bounds how long a command waits for a free slot before it
// gives up and says the bot is busy.
//
// It is short because it is measured against Discord's deadline, not ours: an
// interaction must be acknowledged within three seconds of being created, and
// a command that has not started by then cannot answer at all. Better to
// refuse in a way the user can read than to succeed into a token that has
// already expired.
const slotWaitBudget = 2 * time.Second

func (b *Bot) setSessionContext(ctx context.Context) { b.sessionCtx.Store(&ctx) }

func (b *Bot) baseSessionContext() context.Context {
	if ctx := b.sessionCtx.Load(); ctx != nil && *ctx != nil {
		return *ctx
	}
	return context.Background()
}

// commandContext is the session's context, cancelled when the session ends.
// It carries no deadline: nothing downstream of here takes a context, so one
// would only be a claim.
func (b *Bot) commandContext() (context.Context, context.CancelFunc) {
	return context.WithCancel(b.baseSessionContext())
}

func (b *Bot) acquireCommandSlot(ctx context.Context) error {
	return b.commands.Acquire(ctx)
}

func (b *Bot) releaseCommandSlot() {
	b.commands.Release()
}

func (b *Bot) isGuildBlacklisted(guildID string) bool {
	return slices.Contains(b.cfg.DiscordGuildBlacklist, guildID)
}
