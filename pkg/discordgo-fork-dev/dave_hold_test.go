package discordgo

import (
	"testing"

	"github.com/disgoorg/godave"
)

// stubSession answers the one question the send path asks of a DAVE session.
type stubSession struct {
	godave.Session
	ready bool
}

func (s *stubSession) Ready() bool { return s.ready }

// The send path used to fall straight through to plaintext whenever DAVE was
// not active, which on a channel that requires encryption is audio leaving the
// bot in the clear. These pin the one decision that prevents it.

// No session at all means the channel never negotiated encryption, and
// plaintext is correct there: holding would silence every ordinary voice
// channel that DAVE does not cover.
func TestHoldFramesAllowsAChannelWithoutEncryption(t *testing.T) {
	if holdFrames(nil) {
		t.Fatal("a channel with no DAVE session must still carry audio")
	}
}

// A session exists, so the channel is encrypted — and until an epoch is active
// there is nothing to encrypt with.
func TestHoldFramesStopsSendingWithoutAnEpoch(t *testing.T) {
	if !holdFrames(&stubSession{ready: false}) {
		t.Fatal("a DAVE channel with no epoch must not receive plaintext")
	}
}

// And once the group is up, audio has to flow again — holding forever would be
// the same outage by a different route.
func TestHoldFramesSendsOnceAnEpochIsLive(t *testing.T) {
	if holdFrames(&stubSession{ready: true}) {
		t.Fatal("an established epoch must send")
	}
}

// The channel is encrypted and nobody configured an implementation. Leaving
// v.dave nil would read as "not encrypted" and put plaintext on the wire, so
// there is a session that is never ready and refuses to encrypt by any other
// route. Connected and silent is the safe direction.
func TestUnconfiguredSessionHoldsEverything(t *testing.T) {
	var session godave.Session = unconfiguredSession{}

	if session.Ready() {
		t.Error("reported ready with no implementation behind it")
	}
	if !holdFrames(session) {
		t.Error("a DAVE channel with no implementation must not receive plaintext")
	}
	if _, err := session.Encrypt(1, []byte{0x01}, make([]byte, 64)); err == nil {
		t.Error("encrypted a frame with no implementation")
	}
	if _, err := session.Decrypt("1", []byte{0x01}, make([]byte, 64)); err == nil {
		t.Error("decrypted a frame with no implementation")
	}
}
