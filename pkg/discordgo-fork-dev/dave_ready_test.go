package discordgo

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
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

// A new session offers end-to-end encryption. Declining it is a deliberate
// choice a caller makes for a network that cannot carry the key exchange, and
// it costs everyone in the channel their encryption — so it must never become
// the default by accident.
func TestDAVEIsAdvertisedByDefault(t *testing.T) {
	s, err := New("Bot test-token")
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	if s.MaxDAVEProtocolVersion != 1 {
		t.Fatalf("MaxDAVEProtocolVersion = %d, want 1: a new session must offer encryption", s.MaxDAVEProtocolVersion)
	}
}

// And declining is possible, which is the whole point of the field.
func TestDAVECanBeDeclined(t *testing.T) {
	s, err := New("Bot test-token")
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	s.MaxDAVEProtocolVersion = 0
	if s.MaxDAVEProtocolVersion != 0 {
		t.Fatal("the advertised version should be settable before Open")
	}
}

// readyCipher makes CanEncrypt report true, the way an established MLS group
// would.
func readyCipher(t *testing.T) cipher.AEAD {
	t.Helper()
	block, err := aes.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatalf("aes: %v", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("gcm: %v", err)
	}
	return aead
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
	v.dave = NewDAVESession("user-id") // no group: CanEncrypt stays false

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
	dave := NewDAVESession("user-id")
	v.dave = dave

	go func() {
		time.Sleep(20 * time.Millisecond)
		dave.mu.Lock()
		dave.active = true
		dave.frameCipher = readyCipher(t)
		dave.mu.Unlock()
		v.Cond.Broadcast()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := v.WaitForDAVEReady(ctx); err != nil {
		t.Fatalf("should have woken when the group came up: %v", err)
	}
	if !dave.CanEncrypt() {
		t.Fatal("returned before the session could encrypt")
	}
}
