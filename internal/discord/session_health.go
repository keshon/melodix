package discord

import (
	"context"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/internal/discord/watchdog"
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

// sessionLockProbeTimeout bounds every read of session state below. It is
// deliberately far past any contention: a legitimate reconnect holds the
// session write lock across a dial, two gateway reads and the one-second sleep
// inside CloseWithCode, so a few seconds is normal and thirty is not.
const sessionLockProbeTimeout = 30 * time.Second

// lastHeartbeatAck reads the session's last heartbeat ACK under the lock that
// actually guards it, and gives up if that lock does not come free.
//
// The bool is false on give-up, and callers must treat it as terminal rather
// than retry: discordgo holds the session write lock across gateway reads that
// carry no deadline (Open reads two packets under it), so a socket that
// black-holes parks every reader here until the kernel abandons the
// connection. Both watchers below read this on a timer, so without the timeout
// they park with it — which is exactly how one session ran 22 hours with a
// dead gateway and not one line in the log: the two watchdogs that existed to
// report it were queued behind the same mutex. Measured against the live
// server-domme log of 2026-09-07, not inferred.
//
// The probe goroutine is abandoned rather than cancelled, because it is parked
// in the runtime and there is nothing to interrupt. That costs one goroutine
// per give-up, which is the other reason callers stop after the first.
//
// Do not reach for dg.HeartbeatLatency() here instead. The vendored fork makes
// it race-free (upstream reads its two timestamps under different locks), but
// it still reports the last *completed* exchange, so on a dead connection it
// goes stale and then negative rather than growing — the opposite of what a
// staleness check needs.
func lastHeartbeatAck(dg *discordgo.Session, timeout time.Duration) (time.Time, bool) {
	ack := make(chan time.Time, 1)
	go func() {
		dg.RLock()
		defer dg.RUnlock()
		ack <- dg.LastHeartbeatAck
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case v := <-ack:
		return v, true
	case <-timer.C:
		return time.Time{}, false
	}
}

func (b *Bot) startSessionHealthWatchers(
	sessionCtx context.Context,
	dg *discordgo.Session,
	tracker *watchdog.Tracker,
	notifyUnhealthy func(),
) {
	go watchdog.NewWSSilence(
		tracker,
		b.cfg.WSSilenceTimeout,
		// No latency source: the watchdog decides on staleness, and a latency
		// is the wrong shape for that — see lastHeartbeatAck.
		nil,
		func(meta watchdog.WSSilenceMeta) {
			b.log.Warn().
				Dur("since_last_ws", meta.SinceLastWS).
				Dur("since_last_heartbeat_ack", meta.SinceLastHeartbeatAck).
				Dur("timeout", meta.Timeout).
				Bool("session_lock_wedged", meta.SessionLockWedged).
				Msg("gateway_silent")
			notifyUnhealthy()
		},
		watchdog.WSSilenceOptions{
			SettleDelay: 15 * time.Second,
			Tick:        10 * time.Second,
			LastHeartbeatAck: func() (time.Time, bool) {
				return lastHeartbeatAck(dg, sessionLockProbeTimeout)
			},
		},
	).Run(sessionCtx)

	go func() {
		select {
		case <-sessionCtx.Done():
			return
		case <-time.After(15 * time.Second):
		}

		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		fails := 0

		for {
			select {
			case <-sessionCtx.Done():
				return
			case <-ticker.C:
				// This probe runs every 30s against a lock the WS-silence
				// watcher only touches after its own timeout has elapsed, so
				// it is the faster of the two at spotting a wedged session.
				ack, ok := lastHeartbeatAck(dg, sessionLockProbeTimeout)
				if !ok {
					b.log.Warn().
						Dur("timeout", sessionLockProbeTimeout).
						Msg("session_lock_wedged")
					notifyUnhealthy()
					return
				}
				if ack.IsZero() {
					// Connected but not yet ACKed: a probe now would report a
					// failure that says nothing about the session.
					b.log.Debug().Msg("heartbeat_ack_pending")
					continue
				}
				sinceAck := time.Since(ack)
				if _, err := dg.User("@me"); err != nil {
					fails++
					b.log.Warn().Int("fails", fails).Err(err).Msg("api_probe_failed")
					if fails >= 3 {
						b.log.Warn().Int("fails", fails).Msg("api_probe_threshold")
						notifyUnhealthy()
						return
					}
				} else {
					if fails > 0 {
						b.log.Info().Int("fails", fails).Msg("api_probe_recovered")
					}
					fails = 0
					b.log.Debug().Dur("since_last_heartbeat_ack", sinceAck).Msg("heartbeat_ack")
				}
			}
		}
	}()
}
