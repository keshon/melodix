package discord

import (
	"context"
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/commandkit"
	"github.com/keshon/melodix/internal/discord/commandlogger"
	"github.com/keshon/melodix/internal/discord/commandsync"
	"github.com/keshon/melodix/internal/discord/execguard"
	"github.com/keshon/melodix/internal/discord/watchdog"
)

// RunSession opens one Discord session and blocks until ctx is cancelled or the API probe
// decides the session is unhealthy (transient gateway reconnects do not exit this function).
func (b *Bot) RunSession(ctx context.Context) error {
	dg, err := discordgo.New("Bot " + b.cfg.DiscordToken)
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	dg.LogLevel = discordgo.LogInformational

	b.mu.Lock()
	b.dg = dg
	b.cmdLogger = commandlogger.NewLogger(dg, b.storage, b.log)
	b.cmdSyncer = commandsync.NewSyncer(dg, commandkit.DefaultRegistry, b.log)
	attachDiscordgoLogger(b.log)
	b.mu.Unlock()

	b.cmdGuard.Store(&cmdGuardHolder{g: execguard.New(b.cfg.CommandTimeout, b.cfg.CommandParallelism)})

	tracker := watchdog.NewTracker()
	disconnected := make(chan struct{})
	notifyUnhealthy := b.makeSessionUnhealthyNotifier(disconnected)

	b.wireSessionHandlers(dg, tracker)

	sessionCtx, cancelSession := context.WithCancel(ctx)
	b.sessionCtx.Store(&sessionCtxHolder{ctx: sessionCtx})
	defer func() {
		cancelSession()
		b.sessionCtx.Store(&sessionCtxHolder{ctx: context.Background()})
		b.cmdGuard.Store(&cmdGuardHolder{g: disabledGuard})
	}()

	if err := dg.Open(); err != nil {
		return fmt.Errorf("failed to open Discord session: %w", err)
	}
	defer func() {
		b.log.Info().Msg("discord_session_close")
		dg.Close()
	}()

	b.startSessionHealthWatchers(sessionCtx, dg, tracker, notifyUnhealthy)

	select {
	case <-ctx.Done():
		b.log.Info().Msg("shutdown_signal_received")
		b.stopAllPlayers()
		return nil
	case <-disconnected:
		return fmt.Errorf("%w: websocket disconnected", ErrSessionUnhealthy)
	}
}
