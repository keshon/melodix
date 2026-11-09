package session

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/gateway"
)

// Exercise disgo itself: its background retry loop does not treat a generic
// context cancellation or persistence error as a terminal error.
func TestDisgoStopsRetiredAndFailedClients(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "retired", true: "persistence-failure"}[failed], func(t *testing.T) {
			r, _ := testRecovery(t)
			lifetime, cancel := context.WithCancel(context.Background())
			defer cancel()
			var want error
			if failed {
				want = errors.New("cannot persist recovery budget")
				r.fatal = want
			} else {
				cancel()
				want = context.Canceled
			}
			var dials atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				dials.Add(1)
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			defer server.Close()
			g := gateway.New("unused", func(gateway.Gateway, gateway.EventType, int, gateway.EventData) {},
				gateway.WithURL("ws"+strings.TrimPrefix(server.URL, "http")),
				gateway.WithIdentifyRateLimiter(&sessionLimiter{Recovery: r, lifetime: lifetime}),
				gateway.WithLogger(SlogLogger(r.log)),
			)
			ctx, done := context.WithTimeout(context.Background(), 2500*time.Millisecond)
			defer done()
			err := g.Open(ctx)
			if !errors.Is(err, want) || !errors.Is(err, discord.ErrGatewayAlreadyConnected) {
				t.Fatalf("disgo kept retrying: %v", err)
			}
			if dials.Load() != 0 {
				t.Fatalf("retired client made %d network attempts", dials.Load())
			}
		})
	}
}

// A resumable session reaches the socket without reserving a fresh session,
// even when the entire fresh-session allowance is unavailable.
func TestDisgoResumeBypassesExhaustedFreshBudget(t *testing.T) {
	r, now := testRecovery(t)
	r.state.RemoteTotal = 1000
	r.state.RemoteUsed = 1000
	r.state.RemoteReset = now.Add(time.Hour)
	var dials atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		dials.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	g := gateway.New("unused", func(gateway.Gateway, gateway.EventType, int, gateway.EventData) {},
		gateway.WithURL("ws"+strings.TrimPrefix(server.URL, "http")),
		gateway.WithSessionID("resume-me"), gateway.WithSequence(123),
		gateway.WithIdentifyRateLimiter(&sessionLimiter{Recovery: r, lifetime: context.Background()}),
		gateway.WithLogger(SlogLogger(r.log)),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	if err := g.Open(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unexpected open result: %v", err)
	}
	if dials.Load() != 1 || len(r.state.Attempts) != 0 {
		t.Fatalf("resume dials=%d, fresh reservations=%d", dials.Load(), len(r.state.Attempts))
	}
}

func TestSessionRequiresPersistentRecovery(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Fatal("session accepted an unprotected Gateway")
	}
}
