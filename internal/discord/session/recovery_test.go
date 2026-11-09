package session

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

func testRecovery(t *testing.T) (*Recovery, *time.Time) {
	t.Helper()
	r, err := NewRecovery(context.Background(), t.TempDir(), "unused", 1000, zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	r.now = func() time.Time { return now }
	r.fetch = func(context.Context) (discord.SessionStartLimit, error) {
		return discord.SessionStartLimit{Total: 1000, Remaining: 1000, ResetAfter: 86400000, MaxConcurrency: 1}, nil
	}
	return r, &now
}

func TestRecoveryAdaptiveSchedule(t *testing.T) {
	for _, tc := range []struct {
		used  int
		stage string
		delay time.Duration
	}{
		{749, "normal", 5 * time.Second}, {750, "throttled", 5 * time.Second},
		{825, "throttled", 152500 * time.Millisecond}, {900, "degraded", 5 * time.Minute},
		{940, "degraded", 13 * time.Minute}, {950, "paused", time.Hour},
	} {
		t.Run(fmt.Sprint(tc.used), func(t *testing.T) {
			r, now := testRecovery(t)
			r.state = recoveryState{Attempts: []time.Time{*now}, Next: now.Add(5 * time.Second), RemoteTotal: 1000, RemoteUsed: tc.used, RemoteReset: now.Add(time.Hour)}
			next, stage := r.schedule(*now)
			if stage != tc.stage || absDuration(next.Sub(*now)-tc.delay) > time.Millisecond {
				t.Fatalf("got %s %v, want %s %v", stage, next.Sub(*now), tc.stage, tc.delay)
			}
		})
	}
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func TestLocalBudgetAndBurstSurviveRestart(t *testing.T) {
	r, now := testRecovery(t)
	r.limit = 20
	for i := 0; i < 19; i++ {
		r.state.Attempts = append(r.state.Attempts, now.Add(-time.Duration(19-i)*time.Hour))
	}
	if err := r.save(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewRecovery(context.Background(), filepath.Dir(r.path), "unused", 20, zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	next, stage := reloaded.schedule(*now)
	if stage != "paused" || !next.Equal(now.Add(5*time.Hour)) {
		t.Fatalf("lost rolling budget: %s %v", stage, next)
	}
	// A restart must also preserve the early brake, even far below 75%.
	r.state.Attempts = nil
	for i := 0; i < 5; i++ {
		r.state.Attempts = append(r.state.Attempts, now.Add(time.Duration(i-4)*time.Minute))
	}
	if err := r.save(); err != nil {
		t.Fatal(err)
	}
	reloaded, err = NewRecovery(context.Background(), filepath.Dir(r.path), "unused", 1000, zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	next, stage = reloaded.schedule(*now)
	if stage != "cooldown" || !next.Equal(now.Add(15*time.Minute)) {
		t.Fatalf("lost burst brake: %s %v", stage, next)
	}
	next, stage = reloaded.schedule(now.Add(11 * time.Minute))
	if stage != "cooldown" || !next.Equal(now.Add(15*time.Minute)) {
		t.Fatalf("restart shortened cooldown: %s %v", stage, next)
	}
}

func TestConcurrentHandshakesShareAdmissionSlot(t *testing.T) {
	r, now := testRecovery(t)
	if err := r.Wait(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := r.Wait(ctx, 0); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("overlapping handshake admitted: %v", err)
	}
	if len(r.state.Attempts) != 1 {
		t.Fatal("overlapping handshake spent a reservation")
	}
	r.Unlock(0)
}

func TestReservationIsDurableBeforeAdmission(t *testing.T) {
	r, now := testRecovery(t)
	for i := 0; i < 3; i++ {
		if err := r.Wait(context.Background(), 0); err != nil {
			t.Fatal(err)
		}
		r.Unlock(0)
		*now = now.Add(6 * time.Second)
	}
	reloaded, err := NewRecovery(context.Background(), filepath.Dir(r.path), "unused", 1000, zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.state.Attempts) != 3 || reloaded.state.RemoteUsed != 3 {
		t.Fatalf("lost reservations: %+v", reloaded.state)
	}
	// A stale server response cannot restore already-reserved capacity.
	*now = now.Add(time.Minute)
	if err := r.Wait(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if r.state.RemoteUsed != 4 {
		t.Fatalf("stale server lookup erased reservations: %d", r.state.RemoteUsed)
	}
}

func TestWaitCancelsWithoutSpendingBudget(t *testing.T) {
	r, now := testRecovery(t)
	r.state.Next = now.Add(time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Wait(ctx, 0) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("wait ignored cancellation")
	}
	if len(r.state.Attempts) != 0 {
		t.Fatal("cancellation spent budget")
	}
}

func TestClosedClientCannotReserveAgain(t *testing.T) {
	r, _ := testRecovery(t)
	lifetime, cancel := context.WithCancel(context.Background())
	l := &sessionLimiter{Recovery: r, lifetime: lifetime}
	cancel()
	if err := l.Wait(context.Background(), 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("closed client admitted: %v", err)
	}
	if len(r.state.Attempts) != 0 {
		t.Fatal("closed client spent budget")
	}
}

func TestClosingClientInterruptsPendingAdmission(t *testing.T) {
	r, now := testRecovery(t)
	r.state.Next = now.Add(time.Hour)
	lifetime, cancel := context.WithCancel(context.Background())
	l := &sessionLimiter{Recovery: r, lifetime: lifetime}
	done := make(chan error, 1)
	go func() { done <- l.Wait(context.Background(), 0) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("closed client kept waiting")
	}
}

func TestRecoveryPresenceClearsAfterWindowExpires(t *testing.T) {
	r, now := testRecovery(t)
	r.state.RemoteTotal = 1000
	r.state.RemoteUsed = 800
	r.state.RemoteReset = now.Add(time.Hour)
	if got := r.Presence(); got != "Connection budget low | retries slowed" {
		t.Fatalf("unexpected presence: %q", got)
	}
	*now = now.Add(2 * time.Hour)
	if got := r.Presence(); got != "" {
		t.Fatalf("stale warning: %q", got)
	}
}

func TestEligibilityDoesNotSpendAReservation(t *testing.T) {
	r, _ := testRecovery(t)
	if err := r.WaitUntilEligible(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.state.Attempts) != 0 {
		t.Fatal("preflight spent a reservation")
	}
	if err := r.Wait(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if len(r.state.Attempts) != 1 {
		t.Fatal("gateway did not reserve")
	}
}

func TestUnknownBudgetAndPersistenceFailureBlockAdmission(t *testing.T) {
	r, _ := testRecovery(t)
	r.fetch = func(context.Context) (discord.SessionStartLimit, error) {
		return discord.SessionStartLimit{}, errors.New("offline")
	}
	if err := r.Wait(context.Background(), 0); err == nil {
		t.Fatal("admitted without Discord budget")
	}
	if len(r.state.Attempts) != 0 {
		t.Fatal("budget lookup failure admitted a reservation")
	}
	r, _ = testRecovery(t)
	r.path = filepath.Join(r.path, "missing", "state.json")
	if err := r.Wait(context.Background(), 0); err == nil {
		t.Fatal("admitted without durable state")
	}
	if r.Fatal() == nil {
		t.Fatal("persistence failure did not stop recovery")
	}
}

func TestCorruptRecoveryStateFailsClosed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "discord-recovery.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRecovery(context.Background(), dir, "unused", 1000, zerolog.Nop()); err == nil {
		t.Fatal("ignored corrupt state")
	}
}

func TestTerminalConnectionErrors(t *testing.T) {
	for _, code := range []int{4004, 4010, 4011, 4012, 4013, 4014} {
		if !IsTerminal(fmt.Errorf("wrapped: %w", &websocket.CloseError{Code: code})) {
			t.Fatalf("retrying code %d", code)
		}
	}
	if IsTerminal(&websocket.CloseError{Code: 4009}) {
		t.Fatal("session timeout should recover")
	}
	if !IsTerminal(&rest.Error{Response: &http.Response{StatusCode: 401}}) {
		t.Fatal("retrying invalid token")
	}
}
