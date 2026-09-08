package watchdog

import (
	"context"
	"time"
)

type WSSilenceMeta struct {
	SinceLastWS           time.Duration
	SinceLastHeartbeatAck time.Duration
	HeartbeatLatency      time.Duration
	Timeout               time.Duration
	// SessionLockWedged reports that the heartbeat ACK could not be read at
	// all, rather than that it was read and found stale. Nothing releases a
	// wedged session mutex, so the watcher treats it as terminal instead of
	// waiting for a staleness threshold that will never be evaluated.
	SessionLockWedged bool
}

// WSSilence restarts a session when the gateway receive loop appears silent.
//
// Behavior is intentionally simple:
// - waits settleDelay before starting checks
// - ticks every tick interval
// - does nothing until tracker reports ready
// - triggers unhealthy when both dispatch traffic and heartbeat ACKs are stale
// - triggers unhealthy when the ACK cannot be read at all
// - preserves dispatch-only behavior when no heartbeat ACK source is configured
type WSSilence struct {
	tracker     *Tracker
	timeout     time.Duration
	settleDelay time.Duration
	tick        time.Duration

	heartbeatLatency func() time.Duration
	lastHeartbeatAck func() (time.Time, bool)
	onUnhealthy      func(meta WSSilenceMeta)
}

type WSSilenceOptions struct {
	SettleDelay time.Duration
	Tick        time.Duration
	// LastHeartbeatAck reads the session's last heartbeat ACK, reporting false
	// when it could not complete the read. A source that can block forever
	// must return false rather than wait: this watcher is the thing that
	// notices a dead gateway, so blocking it blinds the bot instead of
	// delaying it.
	LastHeartbeatAck func() (time.Time, bool)
}

func NewWSSilence(tracker *Tracker, timeout time.Duration, heartbeatLatency func() time.Duration, onUnhealthy func(meta WSSilenceMeta), opts WSSilenceOptions) *WSSilence {
	if opts.SettleDelay <= 0 {
		opts.SettleDelay = 15 * time.Second
	}
	if opts.Tick <= 0 {
		opts.Tick = 10 * time.Second
	}
	return &WSSilence{
		tracker:          tracker,
		timeout:          timeout,
		settleDelay:      opts.SettleDelay,
		tick:             opts.Tick,
		heartbeatLatency: heartbeatLatency,
		lastHeartbeatAck: opts.LastHeartbeatAck,
		onUnhealthy:      onUnhealthy,
	}
}

func (w *WSSilence) unhealthyMeta(now time.Time) (WSSilenceMeta, bool) {
	if w == nil || w.tracker == nil || w.timeout <= 0 || !w.tracker.IsReady() {
		return WSSilenceMeta{}, false
	}

	sinceWS := w.tracker.SinceLastWS(now)
	if sinceWS <= w.timeout {
		return WSSilenceMeta{}, false
	}

	var sinceHeartbeatAck time.Duration
	if w.lastHeartbeatAck != nil {
		lastAck, ok := w.lastHeartbeatAck()
		if !ok {
			return WSSilenceMeta{
				SinceLastWS:       sinceWS,
				Timeout:           w.timeout,
				SessionLockWedged: true,
			}, true
		}
		if !lastAck.IsZero() {
			if now.Before(lastAck) {
				sinceHeartbeatAck = 0
			} else {
				sinceHeartbeatAck = now.Sub(lastAck)
			}
			if sinceHeartbeatAck <= w.timeout {
				return WSSilenceMeta{}, false
			}
		}
	}

	var latency time.Duration
	if w.heartbeatLatency != nil {
		latency = w.heartbeatLatency()
	}

	return WSSilenceMeta{
		SinceLastWS:           sinceWS,
		SinceLastHeartbeatAck: sinceHeartbeatAck,
		HeartbeatLatency:      latency,
		Timeout:               w.timeout,
	}, true
}

func (w *WSSilence) Run(ctx context.Context) {
	if w == nil || w.tracker == nil || w.timeout <= 0 || w.onUnhealthy == nil {
		return
	}

	select {
	case <-ctx.Done():
		return
	case <-time.After(w.settleDelay):
	}

	ticker := time.NewTicker(w.tick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if meta, unhealthy := w.unhealthyMeta(now); unhealthy {
				w.onUnhealthy(meta)
				return
			}
		}
	}
}
