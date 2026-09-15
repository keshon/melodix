package cmdqueue

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func newTestQueue() *Queue { return New(zerolog.Nop()) }

// Submitting must not wait for the work, or nothing has moved off the gateway
// read goroutine at all.
func TestSubmitReturnsBeforeTheWorkRuns(t *testing.T) {
	q := newTestQueue()
	release := make(chan struct{})
	started := make(chan struct{})

	if ok := q.Submit("g1", func() { close(started); <-release }); !ok {
		t.Fatal("submit refused")
	}

	<-started // the work is running, and Submit already returned to get here
	close(release)
	if !q.Close(timeoutCtx(t, time.Second)) {
		t.Fatal("queue did not drain")
	}
}

// A guild's music is sequential. Two commands in one guild overlapping would
// mean /play and /next racing over one queue and one voice connection.
func TestOneGuildRunsOneCommandAtATime(t *testing.T) {
	q := newTestQueue()
	var live, peak atomic.Int64
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		q.Submit("g1", func() {
			defer wg.Done()
			n := live.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			live.Add(-1)
		})
	}

	wg.Wait()
	if got := peak.Load(); got != 1 {
		t.Fatalf("%d commands ran at once in one guild", got)
	}
	q.Close(timeoutCtx(t, time.Second))
}

// Arrival order within a guild is the order the user typed them in.
func TestOneGuildKeepsArrivalOrder(t *testing.T) {
	q := newTestQueue()
	var mu sync.Mutex
	var seen []int
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		q.Submit("g1", func() {
			defer wg.Done()
			mu.Lock()
			seen = append(seen, i)
			mu.Unlock()
		})
	}
	wg.Wait()

	for i, got := range seen {
		if got != i {
			t.Fatalf("command %d ran in position %d", got, i)
		}
	}
	q.Close(timeoutCtx(t, time.Second))
}

// One guild's slow command must not be every other guild's wait. This is the
// head-of-line blocking that made a playlist resolve in one guild answer an
// interaction in another past its deadline.
func TestGuildsDoNotWaitForEachOther(t *testing.T) {
	q := newTestQueue()
	blocked := make(chan struct{})
	release := make(chan struct{})
	q.Submit("slow", func() { close(blocked); <-release })
	<-blocked

	ran := make(chan struct{})
	q.Submit("other", func() { close(ran) })

	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("a guild waited behind another guild's command")
	}

	close(release)
	q.Close(timeoutCtx(t, time.Second))
}

// A lane with nothing in it is not a lane. A bot in many guilds should not
// accumulate one goroutine and one map entry per guild it has ever served.
func TestIdleLanesAreNotKept(t *testing.T) {
	q := newTestQueue()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		q.Submit(string(rune('a'+i)), wg.Done)
	}
	wg.Wait()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if q.laneCount() == 0 {
			q.Close(timeoutCtx(t, time.Second))
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("%d idle lanes were kept", q.laneCount())
}

// A lane deleted while a new command arrives has to be startable again, or a
// guild goes quiet for the rest of the process's life.
func TestALaneRestartsAfterGoingIdle(t *testing.T) {
	q := newTestQueue()
	for i := 0; i < 200; i++ {
		done := make(chan struct{})
		if ok := q.Submit("g1", func() { close(done) }); !ok {
			t.Fatalf("submit %d refused", i)
		}
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("command %d never ran", i)
		}
	}
	q.Close(timeoutCtx(t, time.Second))
}

// A panicking command takes down its own command and nothing else. On the
// gateway goroutine it took the whole bot.
func TestAPanicDoesNotStopTheLane(t *testing.T) {
	q := newTestQueue()
	q.Submit("g1", func() { panic("boom") })

	ran := make(chan struct{})
	q.Submit("g1", func() { close(ran) })

	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("a panic stopped the guild's lane")
	}
	q.Close(timeoutCtx(t, time.Second))
}

// Shutdown waits for the command already running rather than abandoning a user
// mid-answer, and says no to anything arriving after -- so the caller knows it
// still owes a reply.
func TestCloseDrainsThenRefuses(t *testing.T) {
	q := newTestQueue()
	finished := atomic.Bool{}
	q.Submit("g1", func() {
		time.Sleep(50 * time.Millisecond)
		finished.Store(true)
	})

	if !q.Close(timeoutCtx(t, 5*time.Second)) {
		t.Fatal("Close reported a timeout it did not have")
	}
	if !finished.Load() {
		t.Fatal("Close abandoned a command that was already running")
	}
	if q.Submit("g1", func() {}) {
		t.Fatal("a closed queue accepted work it will never run")
	}
}

// Close must come back even when a command will not, or shutdown hangs on the
// thing it is trying to shut down.
func TestCloseGivesUpOnAWedgedCommand(t *testing.T) {
	q := newTestQueue()
	release := make(chan struct{})
	defer close(release)
	started := make(chan struct{})
	q.Submit("g1", func() { close(started); <-release })
	<-started

	if q.Close(timeoutCtx(t, 100*time.Millisecond)) {
		t.Fatal("Close claimed to have drained a wedged command")
	}
}

func timeoutCtx(t *testing.T, d time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	t.Cleanup(cancel)
	return ctx
}
