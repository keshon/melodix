package discord

import (
	"time"

	"github.com/rs/zerolog"
)

// closeWithin runs a teardown step, gives up waiting for it after timeout, and
// records how long it actually took.
//
// Do NOT replace this with a bare call. Teardown is the last thing a session
// does, so a step that blocks blocks the restart loop in main with it, and the
// bot a watchdog just correctly declared dead never comes back. What leaks
// instead is one parked goroutine and whatever it holds: the next session
// builds its own client and owes this one nothing.
//
// The duration is logged on every step, not only on the slow ones. "Shutdown
// takes a while" is not a report anybody can act on; "voice_close took 9.9s
// and everything else took 40ms" is, and the only way to have that line when
// it is needed is to have written it before it was.
func closeWithin(phase string, timeout time.Duration, log zerolog.Logger, fn func()) {
	started := time.Now()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		log.Info().Str("phase", phase).Dur("took", time.Since(started)).
			Msg("shutdown_phase")
	case <-timer.C:
		log.Warn().Str("phase", phase).Dur("timeout", timeout).
			Msg("shutdown_phase_abandoned")
	}
}
