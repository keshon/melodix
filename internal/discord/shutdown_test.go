package discord

import (
	"io"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// Teardown is the last thing a session does, so a step that blocks strands the
// restart loop in main and the bot a watchdog just declared dead never comes
// back. disgo's Close walks the voice manager, the gateway and the REST rate
// limiter in turn, each waiting on the last, so any one of them waiting on a
// server that has stopped answering is this case.
func TestCloseWithinAbandonsABlockedStep(t *testing.T) {
	log := zerolog.New(io.Discard)
	blocked := make(chan struct{})
	defer close(blocked)

	start := time.Now()
	closeWithin("voice_close", 50*time.Millisecond, log, func() {
		<-blocked
	})

	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("returned after %s — the timeout is not bounding the step", elapsed)
	}
}

// A step that finishes must not be made to wait out its own timeout, or every
// shutdown costs the worst case.
func TestCloseWithinReturnsAsSoonAsTheStepDoes(t *testing.T) {
	log := zerolog.New(io.Discard)

	start := time.Now()
	closeWithin("noop", 30*time.Second, log, func() {})

	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("a step that returned immediately took %s", elapsed)
	}
}
