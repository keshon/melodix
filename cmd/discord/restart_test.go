package main

import (
	"testing"
	"time"
)

// The outage this was written for: Discord answered the gateway handshake with
// 503 for minutes, and melodix reconnected every five seconds for the whole of
// it. The delay has to grow, or the bot is hammering a service that is already
// saying it cannot cope.
func TestAnOutageBacksOff(t *testing.T) {
	var b restartBackoff

	// Each session dies inside its own open timeout, so none of them counts as
	// the connection having worked.
	var last time.Duration
	for attempt := 1; attempt <= 4; attempt++ {
		delay := b.next(30*time.Second, false)
		if delay <= last {
			t.Fatalf("attempt %d waited %v, no longer than the %v before it", attempt, delay, last)
		}
		last = delay
	}
}

// A ceiling, because a music bot that stays away for an hour after Discord
// comes back is no better than one that never left.
func TestTheBackoffIsCapped(t *testing.T) {
	var b restartBackoff
	for i := 0; i < 50; i++ {
		if delay := b.next(30*time.Second, false); delay > restartMaxDelay {
			t.Fatalf("waited %v, past the %v cap", delay, restartMaxDelay)
		}
	}
}

// The other edge: a session that connected and ran for hours before dropping
// must not inherit the delay from an outage that ended long ago.
func TestASessionThatWorkedResetsTheBackoff(t *testing.T) {
	var b restartBackoff
	for i := 0; i < 6; i++ {
		b.next(30*time.Second, false)
	}

	delay := b.next(3*time.Hour, false)
	if delay > restartBaseDelay {
		t.Fatalf("a session that ran three hours waited %v; want no more than %v", delay, restartBaseDelay)
	}
	if b.failures != 1 {
		t.Fatalf("failures is %d after a healthy session; want the run reset", b.failures)
	}
}

// A session dying inside its own thirty-second open timeout has not proved
// anything about the connection, so it must not count as healthy -- otherwise
// every failed open resets the backoff and there is no backoff.
func TestAFailedOpenDoesNotCountAsAWorkingSession(t *testing.T) {
	var b restartBackoff
	for i := 0; i < 5; i++ {
		b.next(30*time.Second, false)
	}
	if b.failures != 5 {
		t.Fatalf("failures is %d; want every failed open counted", b.failures)
	}
}

// The watchdog's restart is a different thing: the gateway was fine and the
// session stopped answering, so rebuilding is the fix and waiting only extends
// the outage the watchdog just detected.
func TestAWatchdogRestartGoesStraightBack(t *testing.T) {
	var b restartBackoff
	for i := 0; i < 5; i++ {
		b.next(30*time.Second, false)
	}

	if delay := b.next(2*time.Hour, true); delay >= time.Second {
		t.Fatalf("a watchdog restart waited %v; want it to go straight back", delay)
	}
}
