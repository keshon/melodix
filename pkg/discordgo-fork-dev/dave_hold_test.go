package discordgo

import "testing"

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

// A session exists, so the channel is encrypted — and until an epoch is
// active there is nothing to encrypt with.
func TestHoldFramesStopsSendingWithoutAnEpoch(t *testing.T) {
	dave := NewDAVESession("1")
	if !holdFrames(dave) {
		t.Fatal("a DAVE channel with no epoch must not receive plaintext")
	}
}

// The state HandlePrepareEpoch leaves behind when a listener leaves: the
// session is kept, everything it can encrypt with is gone. This is the case
// that was sending in the clear for as long as the bot was alone.
func TestHoldFramesStopsSendingAfterTheEpochIsTornDown(t *testing.T) {
	dave := NewDAVESession("1")
	dave.active = true
	dave.frameCipher = readyCipher(t)
	if holdFrames(dave) {
		t.Fatal("an established epoch must send")
	}

	if _, err := dave.HandlePrepareEpoch(1, 1); err != nil {
		t.Fatalf("prepare epoch: %v", err)
	}
	if !holdFrames(dave) {
		t.Fatal("the epoch is gone, so the frames have to stop")
	}
}
