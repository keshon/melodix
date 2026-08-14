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
}

// WSSilence restarts a session when the gateway receive loop appears silent.
//
// Behavior is intentionally simple:
// - waits settleDelay before starting checks
// - ticks every tick interval
// - does nothing until tracker reports ready
// - triggers unhealthy when both dispatch traffic and heartbeat ACKs are stale
// - preserves dispatch-only behavior when no heartbeat ACK source is configured
type WSSilence struct {
	tracker     *Tracker
	timeout     time.Duration
	settleDelay time.Duration
	tick        time.Duration

	heartbeatLatency func() time.Duration
	lastHeartbeatAck func() time.Time
	onUnhealthy      func(meta WSSilenceMeta)
}

type WSSilenceOptions struct {
	SettleDelay      time.Duration
	Tick             time.Duration
	LastHeartbeatAck func() time.Time
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
		lastAck := w.lastHeartbeatAck()
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
