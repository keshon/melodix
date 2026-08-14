package watchdog

import (
	"testing"
	"time"
)

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
		WSSilenceOptions{LastHeartbeatAck: func() time.Time { return now.Add(-10 * time.Second) }},
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
		WSSilenceOptions{LastHeartbeatAck: func() time.Time { return now.Add(-4 * time.Minute) }},
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
		WSSilenceOptions{LastHeartbeatAck: func() time.Time { return time.Time{} }},
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
