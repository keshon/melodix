package discord

import (
	"time"

	"github.com/rs/zerolog"
)

// sessionCloseTimeout is the outer bound: how long teardown may take before
// it is abandoned and the process moves on regardless.
const sessionCloseTimeout = 15 * time.Second

// gatewayCloseBudget is what the library's own Close is given, and it is
// deliberately much shorter than the abandon above.
//
// disgo's Close walks the voice manager, then the gateway, then REST, each
// waiting on the last. The last two close by taking their rate limiter's lock
// with this context -- and a bucket holds that lock while it sleeps out a
// rate-limit window. Hand that a generous budget and shutdown sits there
// waiting for a reset that only matters to requests nobody is going to make,
// because the process is leaving.
//
// Three seconds is far more than a voice disconnect and a websocket close
// frame need, and far less than a rate-limit window. If a disconnect ever
// looks like it was cut short, this is the number that cut it.
const gatewayCloseBudget = 3 * time.Second

// commandsDrainTimeout bounds waiting for the commands already running to
// finish. A command is a REST round trip or two plus whatever the engine does
// to start a track; anything past this is a command that is not coming back,
// and the process is leaving either way.
const commandsDrainTimeout = 5 * time.Second

// playersStopTimeout bounds stopping playback across every guild. Each player
// leaves its voice channel, which is a round trip, and they are stopped one
// after another -- so a server that has stopped answering costs this once
// rather than once per guild.
const playersStopTimeout = 10 * time.Second

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
