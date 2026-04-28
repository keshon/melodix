package watchdog

import (
	"context"
	"time"
)

type WSSilenceMeta struct {
	SinceLastWS      time.Duration
	HeartbeatLatency time.Duration
	Timeout          time.Duration
}

// WSSilence restarts a session when the gateway receive loop appears silent.
//
// Behavior is intentionally simple and mirrors the old inline logic:
// - waits settleDelay before starting checks
// - ticks every tick interval
// - does nothing until tracker reports ready
// - triggers unhealthy when time since last WS exceeds timeout
type WSSilence struct {
	tracker     *Tracker
	timeout     time.Duration
	settleDelay time.Duration
	tick        time.Duration

	heartbeatLatency func() time.Duration
	onUnhealthy       func(meta WSSilenceMeta)
}

type WSSilenceOptions struct {
	SettleDelay time.Duration
	Tick        time.Duration
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
		onUnhealthy:      onUnhealthy,
	}
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
			if !w.tracker.IsReady() {
				continue
			}
			since := w.tracker.SinceLastWS(now)
			if since > w.timeout {
				lat := time.Duration(0)
				if w.heartbeatLatency != nil {
					lat = w.heartbeatLatency()
				}
				w.onUnhealthy(WSSilenceMeta{
					SinceLastWS:      since,
					HeartbeatLatency: lat,
					Timeout:          w.timeout,
				})
				return
			}
		}
	}
}

