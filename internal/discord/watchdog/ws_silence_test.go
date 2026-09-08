package watchdog

import (
	"testing"
	"time"
)

func ackAt(t time.Time) func() (time.Time, bool) {
	return func() (time.Time, bool) { return t, true }
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

// A wedged session mutex is the failure that once cost 22 hours of silent
// downtime: the ACK source could not answer, so the watcher that existed to
// report the dead gateway blocked instead. It must now decide without one.
func TestWSSilenceTriggersWhenHeartbeatAckCannotBeRead(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	watcher := NewWSSilence(
		readyTrackerAt(now.Add(-3*time.Minute)),
		2*time.Minute,
		nil,
		nil,
		WSSilenceOptions{LastHeartbeatAck: func() (time.Time, bool) { return time.Time{}, false }},
	)

	meta, unhealthy := watcher.unhealthyMeta(now)
	if !unhealthy {
		t.Fatal("session whose heartbeat ACK could not be read was healthy")
	}
	if !meta.SessionLockWedged {
		t.Fatal("SessionLockWedged = false, want true so the log says why")
	}
	if meta.SinceLastWS != 3*time.Minute {
		t.Fatalf("SinceLastWS = %s, want 3m", meta.SinceLastWS)
	}
}

// The unreadable-ACK path must not swallow the healthy case: an ACK source
// that answers keeps deciding on staleness as before.
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
