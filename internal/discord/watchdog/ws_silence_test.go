package watchdog

import (
	"context"
	"testing"
	"time"
)

func ackAt(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func readyTrackerAt(lastWS time.Time) *Tracker {
	tracker := NewTracker()
	tracker.lastWSNano.Store(lastWS.UnixNano())
	tracker.readyNano.Store(lastWS.UnixNano())
	return tracker
}

func TestWSSilenceKeepsQuietSessionWithFreshHeartbeatHealthy(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	watcher := NewWSSilence(
		readyTrackerAt(now.Add(-3*time.Minute)),
		2*time.Minute,
		func() time.Duration { return 100 * time.Millisecond },
		nil,
		WSSilenceOptions{LastHeartbeatAck: ackAt(now.Add(-10 * time.Second))},
	)

	if meta, unhealthy := watcher.unhealthyMeta(now); unhealthy {
		t.Fatalf("quiet session with a fresh heartbeat was unhealthy: %+v", meta)
	}
}

func TestWSSilenceTriggersWhenDispatchAndHeartbeatAreStale(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	watcher := NewWSSilence(
		readyTrackerAt(now.Add(-3*time.Minute)),
		2*time.Minute,
		func() time.Duration { return 100 * time.Millisecond },
		nil,
		WSSilenceOptions{LastHeartbeatAck: ackAt(now.Add(-4 * time.Minute))},
	)

	meta, unhealthy := watcher.unhealthyMeta(now)
	if !unhealthy {
		t.Fatal("session with stale dispatch and heartbeat was healthy")
	}
	if meta.SinceLastWS != 3*time.Minute {
		t.Fatalf("SinceLastWS = %s, want 3m", meta.SinceLastWS)
	}
	if meta.SinceLastHeartbeatAck != 4*time.Minute {
		t.Fatalf("SinceLastHeartbeatAck = %s, want 4m", meta.SinceLastHeartbeatAck)
	}
}

func TestWSSilencePreservesLegacyBehaviorWithoutHeartbeatSource(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	watcher := NewWSSilence(
		readyTrackerAt(now.Add(-3*time.Minute)),
		2*time.Minute,
		nil,
		nil,
		WSSilenceOptions{},
	)

	if _, unhealthy := watcher.unhealthyMeta(now); !unhealthy {
		t.Fatal("stale dispatch without a heartbeat source was healthy")
	}
}

func TestWSSilencePreservesLegacyBehaviorBeforeFirstHeartbeatAck(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	watcher := NewWSSilence(
		readyTrackerAt(now.Add(-3*time.Minute)),
		2*time.Minute,
		nil,
		nil,
		WSSilenceOptions{LastHeartbeatAck: ackAt(time.Time{})},
	)

	if _, unhealthy := watcher.unhealthyMeta(now); !unhealthy {
		t.Fatal("stale dispatch before the first heartbeat ACK was healthy")
	}
}

func TestWSSilenceWaitsUntilReady(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tracker := NewTracker()
	tracker.lastWSNano.Store(now.Add(-3 * time.Minute).UnixNano())
	watcher := NewWSSilence(tracker, 2*time.Minute, nil, nil, WSSilenceOptions{})

	if _, unhealthy := watcher.unhealthyMeta(now); unhealthy {
		t.Fatal("session was unhealthy before ready")
	}
}

// A session that has connected but never been acknowledged has no staleness
// to measure, so the decision falls to gateway silence alone -- and a gateway
// that has said nothing for three minutes is dead whether or not it ever
// acknowledged anything.
func TestWSSilenceDecidesOnSilenceWhenNothingWasEverAcked(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	watcher := NewWSSilence(
		readyTrackerAt(now.Add(-3*time.Minute)),
		2*time.Minute,
		nil,
		nil,
		WSSilenceOptions{LastHeartbeatAck: ackAt(time.Time{})},
	)

	meta, unhealthy := watcher.unhealthyMeta(now)
	if !unhealthy {
		t.Fatal("a gateway silent for three minutes was called healthy")
	}
	if meta.SinceLastWS != 3*time.Minute {
		t.Fatalf("SinceLastWS = %s, want 3m", meta.SinceLastWS)
	}
}

// An ACK source that answers keeps deciding on staleness as before.
func TestWSSilenceKeepsSessionHealthyWhenAckIsReadable(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	watcher := NewWSSilence(
		readyTrackerAt(now.Add(-3*time.Minute)),
		2*time.Minute,
		nil,
		nil,
		WSSilenceOptions{LastHeartbeatAck: ackAt(now.Add(-1 * time.Second))},
	)

	if meta, unhealthy := watcher.unhealthyMeta(now); unhealthy {
		t.Fatalf("session with a fresh readable ACK was unhealthy: %+v", meta)
	}
}

// A signal is not the end of the watch. Only restart-session ends the session
// it was raised in; ignore, restart-voice and a grace count all leave it
// running, and a watcher that stopped after its first signal left those
// sessions unwatched until the next restart -- which, with grace, never came,
// because the count it was waiting to exceed could not grow past one.
func TestWSSilenceKeepsSignallingWhileTheSilenceLasts(t *testing.T) {
	tracker := readyTrackerAt(time.Now().Add(-time.Hour))
	signals := make(chan struct{}, 16)
	watcher := NewWSSilence(tracker, 20*time.Millisecond, nil,
		func(WSSilenceMeta) { signals <- struct{}{} },
		WSSilenceOptions{SettleDelay: time.Millisecond, Tick: 2 * time.Millisecond},
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go watcher.Run(ctx)

	deadline := time.After(2 * time.Second)
	for got := 0; got < 3; got++ {
		select {
		case <-signals:
		case <-deadline:
			t.Fatalf("got %d signal(s) from a gateway that stayed silent, want at least 3", got)
		}
	}
}

// While the silence lasts, a signal repeats once per timeout rather than on
// every tick: the gateway has not got any deader in ten seconds, and each
// signal can invalidate every guild's voice connection.
func TestWSSilenceSignalsAtMostOncePerTimeout(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	watcher := NewWSSilence(readyTrackerAt(start.Add(-3*time.Minute)), 2*time.Minute, nil, nil, WSSilenceOptions{})

	var last time.Time
	if _, ok := watcher.signal(start, &last); !ok {
		t.Fatal("first tick of a silent gateway did not signal")
	}
	if _, ok := watcher.signal(start.Add(10*time.Second), &last); ok {
		t.Fatal("signalled again one tick later")
	}
	if _, ok := watcher.signal(start.Add(2*time.Minute), &last); !ok {
		t.Fatal("did not signal again once a full timeout had passed")
	}
}

// Recovery re-arms the watch: the next silence is a new outage and is reported
// at once, not held back by the cooldown of the last one.
func TestWSSilenceRearmsWhenTheGatewayRecovers(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	tracker := readyTrackerAt(start.Add(-3 * time.Minute))
	watcher := NewWSSilence(tracker, 2*time.Minute, nil, nil, WSSilenceOptions{})

	var last time.Time
	if _, ok := watcher.signal(start, &last); !ok {
		t.Fatal("silent gateway did not signal")
	}
	tracker.lastWSNano.Store(start.Add(5 * time.Second).UnixNano())
	if _, ok := watcher.signal(start.Add(10*time.Second), &last); ok {
		t.Fatal("signalled while the gateway was talking")
	}
	if _, ok := watcher.signal(start.Add(5*time.Second+3*time.Minute), &last); !ok {
		t.Fatal("a new silence after recovery was not reported")
	}
}
