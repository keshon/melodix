package main

import (
	"math/rand/v2"
	"time"
)

// restartBackoff decides how long to wait before building the next session.
//
// It exists because the loop it replaced waited a flat five seconds forever.
// That is fine for the failure it was written for -- one session ending badly
// -- and wrong for the one actually seen: Discord answering the gateway
// handshake with 503 Service Unavailable for minutes at a stretch. Every cycle
// threw away the client, which threw away disgo's own 1/2/4/8s backoff with
// it, so the outer loop reset the inner one and melodix reconnected at a fixed
// cadence for as long as the outage lasted. Retrying a service that is telling
// you it is overloaded, at a rate that never drops, is how a bot's token stops
// being welcome.
type restartBackoff struct {
	// failures is the run of sessions that did not last, reset by one that
	// did.
	failures int
}

// next reports how long to wait, given how long the session that just ended
// lasted and whether the watchdog is what ended it.
func (b *restartBackoff) next(ranFor time.Duration, unhealthy bool) time.Duration {
	// A session that lived long enough to be doing its job has proved the
	// connection is fine, so whatever just went wrong starts a fresh run
	// rather than inheriting the delay from an outage hours ago.
	if ranFor >= restartHealthyAfter {
		b.failures = 0
	}

	if unhealthy {
		// The watchdog ended a session that was connected and stopped
		// answering. Rebuilding is the fix and the gateway is not the problem,
		// so this goes straight back -- jittered only so that a fleet of bots
		// restarting on the same outage does not arrive together.
		return time.Duration(rand.IntN(200)) * time.Millisecond
	}

	b.failures++
	delay := restartBaseDelay
	for i := 1; i < b.failures && delay < restartMaxDelay; i++ {
		delay *= 2
	}
	if delay > restartMaxDelay {
		delay = restartMaxDelay
	}
	// Jitter downward, so the cap is a ceiling rather than a rendezvous.
	return delay - time.Duration(rand.Int64N(int64(delay/4)))
}

// What the backoff is measured in.
//
// The base is what this waited before there was a backoff at all, so a single
// unlucky session still recovers as quickly as it used to. The ceiling is a
// music bot's patience rather than a server's: a minute of extra silence after
// Discord comes back is already annoying, and the open attempt itself takes up
// to thirty seconds on top. Healthy-after is what counts as the connection
// having worked -- long enough that a session dying inside its own open
// timeout cannot claim it.
const (
	restartBaseDelay    = 5 * time.Second
	restartMaxDelay     = time.Minute
	restartHealthyAfter = time.Minute
)
