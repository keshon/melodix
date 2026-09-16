package discord

import (
	"testing"
	"time"
)

// The silence watcher repeats its signal once per WS_SILENCE_TIMEOUT, and the
// grace count resets whenever a signal lands outside DISCORD_UNHEALTHY_WINDOW.
// A window that cannot hold grace+1 signals therefore never lets the count
// pass grace, and restart-session silently never restarts -- which is what the
// defaults do with any grace at all: a one-minute window, two-minute timeout.
func TestGraceEscalationNeedsAWindowThatHoldsEnoughSignals(t *testing.T) {
	const tick = 10 * time.Second
	cases := []struct {
		grace   int
		window  time.Duration
		timeout time.Duration
		want    bool
	}{
		{grace: 0, window: time.Minute, timeout: 2 * time.Minute, want: true},
		{grace: 1, window: time.Minute, timeout: 2 * time.Minute, want: false},
		// A signal waits for the first tick after the timeout, so exactly one
		// timeout of window is not enough.
		{grace: 1, window: 2 * time.Minute, timeout: 2 * time.Minute, want: false},
		{grace: 1, window: 2*time.Minute + tick, timeout: 2 * time.Minute, want: true},
		{grace: 2, window: 4 * time.Minute, timeout: 2 * time.Minute, want: false},
		{grace: 2, window: 5 * time.Minute, timeout: 2 * time.Minute, want: true},
		{grace: 3, window: time.Minute, timeout: 0, want: true},
	}
	for _, c := range cases {
		if got := graceCanEscalate(c.grace, c.window, c.timeout, tick); got != c.want {
			t.Errorf("graceCanEscalate(%d, %s, %s) = %v, want %v", c.grace, c.window, c.timeout, got, c.want)
		}
	}
}
