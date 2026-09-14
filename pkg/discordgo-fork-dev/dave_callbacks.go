package discordgo

import "github.com/disgoorg/godave"

// daveCallbacks is the send half of the DAVE protocol, in the shape the
// godave.Session implementations expect to find it.
//
// The split is the point. A DAVE session owns MLS state and decides what has to
// be said; this connection owns the websocket and knows how to say it. Neither
// needs to know how the other works, which is what lets the session be chosen
// somewhere else entirely — dave-go today, and whatever replaces it without
// this package noticing.
//
// Every method here is one opcode, and all four already existed as unexported
// senders on VoiceConnection. The only thing that changed is that they report
// failures instead of logging them and returning nothing: an implementation
// that commits has to be able to tell whether its commit reached the gateway.
type daveCallbacks struct {
	conn *VoiceConnection
}

var _ godave.Callbacks = daveCallbacks{}

// SendMLSKeyPackage sends opcode 26, which is how this client asks to be let
// into an epoch.
func (c daveCallbacks) SendMLSKeyPackage(mlsKeyPackage []byte) error {
	return c.conn.sendDAVEKeyPackageBinary(mlsKeyPackage)
}

// SendMLSCommitWelcome sends opcode 28: a commit for the pending proposals and
// a Welcome for whoever the commit adds. The hand-rolled session never calls
// this, because it cannot build either message.
func (c daveCallbacks) SendMLSCommitWelcome(mlsCommitWelcome []byte) error {
	return c.conn.sendDAVECommitWelcome(mlsCommitWelcome)
}

// SendReadyForTransition sends opcode 23, acknowledging that the new epoch can
// be used from here on.
func (c daveCallbacks) SendReadyForTransition(transitionID uint16) error {
	return c.conn.sendDAVEReadyForTransition(transitionID)
}

// SendInvalidCommitWelcome sends opcode 31, rejecting a commit this client
// could not process and asking to be Welcomed in again instead.
func (c daveCallbacks) SendInvalidCommitWelcome(transitionID uint16) error {
	return c.conn.sendDAVEInvalidCommitWelcome(transitionID)
}
