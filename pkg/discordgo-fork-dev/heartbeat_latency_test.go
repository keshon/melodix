package discordgo

import (
	"sync"
	"testing"
	"time"
)

// HeartbeatLatency reads two timestamps that the gateway writes under different
// locks: LastHeartbeatAck under the Session mutex (op 11 in onEvent) and
// LastHeartbeatSent under wsMutex (the heartbeat loop). Reading both directly
// was a data race, and nothing exercised it, so the detector never saw it.
//
// This reproduces both writers alongside concurrent readers. Run with -race:
// before the fix it reports a race on LastHeartbeatAck/LastHeartbeatSent.
func TestHeartbeatLatencyIsRaceFree(t *testing.T) {
	s := &Session{}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// The heartbeat loop: writes the send timestamp under wsMutex.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			s.wsMutex.Lock()
			sentAt := time.Now().UTC()
			s.LastHeartbeatSent = sentAt
			s.lastHeartbeatSentNano.Store(sentAt.UnixNano())
			s.wsMutex.Unlock()
		}
	}()

	// The gateway receive loop: writes the ack timestamp under the Session mutex.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			s.Lock()
			s.LastHeartbeatAck = time.Now().UTC()
			s.Unlock()
		}
	}()

	// Readers: the watchdog and /maintenance ping both call this.
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_ = s.HeartbeatLatency()
			}
		}()
	}

	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// Before the first heartbeat is sent there is nothing to measure, and the
// zero-value subtraction would otherwise report a nonsensical duration.
func TestHeartbeatLatencyBeforeFirstSend(t *testing.T) {
	s := &Session{}
	s.LastHeartbeatAck = time.Now().UTC()
	if got := s.HeartbeatLatency(); got != 0 {
		t.Fatalf("HeartbeatLatency before any send = %v, want 0", got)
	}
}

// A normal exchange reports a positive latency.
func TestHeartbeatLatencyAfterExchange(t *testing.T) {
	s := &Session{}
	sentAt := time.Now().UTC()
	s.LastHeartbeatSent = sentAt
	s.lastHeartbeatSentNano.Store(sentAt.UnixNano())
	s.LastHeartbeatAck = sentAt.Add(120 * time.Millisecond)

	if got := s.HeartbeatLatency(); got != 120*time.Millisecond {
		t.Fatalf("HeartbeatLatency = %v, want 120ms", got)
	}
}
