package discordgo

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// Discord puts a voice channel into end-to-end encryption when every
// participant claims to support it, and from then on expects encrypted frames.
// Sending before the MLS group exists gets the connection closed within
// seconds, and the rejoin re-keys the group — repeat that and nobody in the
// channel can hear anybody. WaitForDAVEReady is the gate that stops it, so
// these cover the three answers it can give.

func newTestVoiceConnection() *VoiceConnection {
	return &VoiceConnection{Cond: sync.NewCond(&sync.Mutex{})}
}

// The ordinary case: no encryption on the channel, so there is nothing to wait
// for and playback must not be delayed by a millisecond.
func TestWaitForDAVEReadyReturnsImmediatelyWithoutEncryption(t *testing.T) {
	v := newTestVoiceConnection()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	start := time.Now()
	if err := v.WaitForDAVEReady(ctx); err != nil {
		t.Fatalf("a channel without DAVE should not wait: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("waited %s on a channel with no encryption", elapsed)
	}
}

// The failure this whole gate exists for. A handshake that never completes must
// surface as an error rather than letting the caller send in the clear, and it
// must give up when the context says so instead of blocking playback forever.
func TestWaitForDAVEReadyGivesUpWhenEncryptionNeverComesUp(t *testing.T) {
	v := newTestVoiceConnection()
	v.dave = &stubSession{ready: false} // no group: never ready

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := v.WaitForDAVEReady(ctx)
	if err == nil {
		t.Fatal("reported ready while the session could not encrypt")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("gave up after %s — the context is not bounding the wait", elapsed)
	}
}

// The success path, and the reason the Cond.Broadcast calls around the MLS
// handling matter: without them the waiter sleeps through the group coming up.
func TestWaitForDAVEReadyReturnsOnceEncryptionComesUp(t *testing.T) {
	v := newTestVoiceConnection()
	dave := &stubSession{ready: false}
	v.dave = dave

	go func() {
		time.Sleep(20 * time.Millisecond)
		dave.ready = true
		v.Cond.Broadcast()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := v.WaitForDAVEReady(ctx); err != nil {
		t.Fatalf("should have woken when the group came up: %v", err)
	}
	if !dave.Ready() {
		t.Fatal("returned before the session could encrypt")
	}
}

// The regression that prompted this test: the identify literal lost its
// MaxDAVEProtocolVersion field during an unrelated edit. It compiled, the
// socket opened, and the field marshalled as 0 — which Discord answers with
// close code 4017, so the bot could not join any voice channel at all. Assert
// the JSON that actually goes on the wire, not the constant behind it.
func TestVoiceHandshakeAdvertisesDAVE(t *testing.T) {
	raw, err := json.Marshal(newVoiceHandshake("guild", "user", "session", "token"))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got struct {
		Data struct {
			Version *int `json:"max_dave_protocol_version"`
		} `json:"d"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Data.Version == nil {
		t.Fatalf("max_dave_protocol_version missing from the identify: %s", raw)
	}
	if *got.Data.Version != 1 {
		t.Fatalf("max_dave_protocol_version = %d, want 1: 0 is refused with close code 4017", *got.Data.Version)
	}
}
