package discord

import (
	"sync"
	"time"
)

func (b *Bot) makeSessionUnhealthyNotifier(disconnected chan struct{}) func() {
	var restartOnce sync.Once
	var unhealthyMu sync.Mutex
	var unhealthyCount int
	var unhealthyWindowStart time.Time

	invalidateSinks := func() {
		if b.voice != nil {
			b.voice.InvalidateAllSinks()
		}
	}

	return func() {
		mode := b.cfg.DiscordUnhealthyMode
		switch mode {
		case "ignore":
			return
		case "restart-voice":
			invalidateSinks()
			return
		case "restart-session", "":
		default:
			b.log.Warn().Str("mode", mode).Msg("discord_unhealthy_mode_unknown")
		}

		grace := b.cfg.DiscordUnhealthyGrace
		if grace < 0 {
			grace = 0
		}
		window := b.cfg.DiscordUnhealthyWindow
		if window <= 0 {
			window = time.Minute
		}

		shouldRestart := true
		if grace > 0 {
			now := time.Now()
			unhealthyMu.Lock()
			if unhealthyWindowStart.IsZero() || now.Sub(unhealthyWindowStart) > window {
				unhealthyWindowStart = now
				unhealthyCount = 0
			}
			unhealthyCount++
			if unhealthyCount <= grace {
				shouldRestart = false
			}
			unhealthyMu.Unlock()
		}

		if !shouldRestart {
			invalidateSinks()
			return
		}

		restartOnce.Do(func() {
			b.log.Warn().Msg("discord_session_unhealthy")
			invalidateSinks()
			close(disconnected)
		})
	}
}
