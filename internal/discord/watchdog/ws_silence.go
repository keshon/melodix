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
// - keeps watching after a signal; see signal for how often it repeats
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
	SettleDelay time.Duration
	Tick        time.Duration
	// LastHeartbeatAck reads the session's last heartbeat ACK, or the zero
	// time before the first one arrives. It must not block: this watcher is
	// the thing that notices a dead gateway, so a source that can wait
	// forever blinds the bot rather than delaying it.
	//
	// It used to be able to answer "I could not read it", because the
	// discordgo fork held the session lock across gateway reads carrying no
	// deadline and a black-holed socket parked every reader -- a read that
	// might never return, which the watcher treated as terminal. disgo
	// delivers the ack as an event, so there is no lock to wedge and no
	// failure to report.
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

	var lastSignal time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if meta, ok := w.signal(now, &lastSignal); ok {
				w.onUnhealthy(meta)
			}
		}
	}
}

// signal decides whether this tick reports the gateway unhealthy. last is the
// time of the previous signal, owned by Run's loop.
//
// The watch does not end at a signal. Only restart-session ends the session it
// was raised in, and that ends this watcher through ctx. Every other outcome --
// ignore, restart-voice, a signal absorbed by DISCORD_UNHEALTHY_GRACE -- leaves
// the session running, and the watcher used to return after its first signal
// regardless, so those sessions went unwatched until the next restart. Under a
// grace count that restart never came: the count it waited to exceed could not
// grow past one.
//
// While a silence lasts it repeats once per timeout, not once per tick: the
// gateway is no deader ten seconds later, and each signal can drop every
// guild's voice connection. A gateway that talks again re-arms it, so the next
// silence is a new outage and is reported at once.
func (w *WSSilence) signal(now time.Time, last *time.Time) (WSSilenceMeta, bool) {
	meta, unhealthy := w.unhealthyMeta(now)
	if !unhealthy {
		*last = time.Time{}
		return WSSilenceMeta{}, false
	}
	if !last.IsZero() && now.Sub(*last) < w.timeout {
		return WSSilenceMeta{}, false
	}
	*last = now
	return meta, true
}
