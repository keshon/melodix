package stream

import (
	"errors"
	"testing"
)

func TestErrVoiceTransportSentinel(t *testing.T) {
	if !errors.Is(ErrVoiceTransport, ErrVoiceTransport) {
		t.Fatal("ErrVoiceTransport must work with errors.Is for player/sink handling")
	}
}
