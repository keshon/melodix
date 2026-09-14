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

// The other way an epoch dies: somebody else commits. This implementation
// cannot process a commit, so it rejects one and asks to be re-Welcomed --
// and until that Welcome arrives the only cipher it has belongs to the epoch
// the group has just left.
func TestHoldFramesStopsSendingOnceTheEpochWeHoldIsDead(t *testing.T) {
	dave := NewDAVESession("1")
	// The state a Welcome leaves behind: an epoch we can send under.
	dave.active = true
	dave.frameCipher = readyCipher(t)
	dave.senderKey = make([]byte, 32)
	dave.exporterSecret = make([]byte, 32)
	if holdFrames(dave) {
		t.Fatal("an established epoch must send")
	}

	// Opcode 29: another member announced a commit, which we answered with
	// invalid_commit_welcome and a fresh key package.
	if _, err := dave.ResetForReWelcome(); err != nil {
		t.Fatalf("reset for re-welcome: %v", err)
	}

	// Opcodes 21 and 22: the group moves into that epoch without us.
	dave.HandlePrepareTransition(7, 1)
	if err := dave.HandleExecuteTransition(7); err != nil {
		t.Fatalf("execute transition: %v", err)
	}

	if !holdFrames(dave) {
		t.Fatal("still sending under the epoch the group left; nobody can decrypt it")
	}
}
