package discordgo

import (
	"errors"
	"testing"
)

// The four senders used to log a failure and return nothing, which is safe only
// for as long as nobody acts on the result. A godave.Session does act on it: it
// decides whether to retry, and whether the epoch it just announced is one the
// gateway has actually heard about. A send that never left must not read as one
// that did — least of all when the websocket is simply gone, which is the case
// the old code was quietest about, logging nothing at all.
func TestDAVECallbacksReportASendWithNoConnection(t *testing.T) {
	callbacks := daveCallbacks{conn: newTestVoiceConnection()}

	sends := []struct {
		name string
		send func() error
	}{
		{"key package", func() error { return callbacks.SendMLSKeyPackage([]byte{0x01}) }},
		{"commit welcome", func() error { return callbacks.SendMLSCommitWelcome([]byte{0x01}) }},
		{"ready for transition", func() error { return callbacks.SendReadyForTransition(7) }},
		{"invalid commit welcome", func() error { return callbacks.SendInvalidCommitWelcome(7) }},
	}

	for _, s := range sends {
		if err := s.send(); !errors.Is(err, errDAVENoConnection) {
			t.Errorf("%s: got %v, want errDAVENoConnection", s.name, err)
		}
	}
}
