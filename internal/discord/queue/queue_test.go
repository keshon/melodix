package queue

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// Uncapped: these tests are about lanes, and the cap has its own below.
func newTestQueue() *Queue { return New(zerolog.Nop(), 0) }

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

	for i := 0; i < maxPerLane/2; i++ {
		wg.Add(1)
		if !q.Submit("g1", func() {
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
		}) {
			t.Fatalf("submit %d refused below the lane's bound", i)
		}
	}

	wg.Wait()
	if got := peak.Load(); got != 1 {
		t.Fatalf("%d commands ran at once in one guild", got)
	}
	q.Close(timeoutCtx(t, time.Second))
}

// Arrival order within a guild is the order the user typed them in.
//
// The lane is held shut while they are queued, so this tests the order they
// come out in rather than the order a race happened to produce.
func TestOneGuildKeepsArrivalOrder(t *testing.T) {
	q := newTestQueue()
	release := make(chan struct{})
	blocked := make(chan struct{})
	if !q.Submit("g1", func() { close(blocked); <-release }) {
		t.Fatal("submit refused")
	}
	<-blocked

	var mu sync.Mutex
	var seen []int
	var wg sync.WaitGroup

	const queued = maxPerLane - 1 // one slot is the blocker's
	for i := 0; i < queued; i++ {
		wg.Add(1)
		if !q.Submit("g1", func() {
			defer wg.Done()
			mu.Lock()
			seen = append(seen, i)
			mu.Unlock()
		}) {
			t.Fatalf("submit %d refused below the lane's bound", i)
		}
	}
	close(release)
	wg.Wait()

	if len(seen) != queued {
		t.Fatalf("%d commands ran, want %d", len(seen), queued)
	}
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

// A lane with no bound trades a blocked gateway for a backlog holding work
// whose right to reply has expired. Refusing at the door is a message the
// caller can read.
func TestALaneRefusesRatherThanGrowWithoutBound(t *testing.T) {
	q := newTestQueue()
	release := make(chan struct{})
	defer close(release)
	blocked := make(chan struct{})
	q.Submit("g1", func() { close(blocked); <-release })
	<-blocked

	accepted := 0
	for i := 0; i < maxPerLane*2; i++ {
		if q.Submit("g1", func() {}) {
			accepted++
		}
	}
	if accepted != maxPerLane {
		t.Fatalf("accepted %d queued commands, want the lane's bound of %d", accepted, maxPerLane)
	}

	// A different guild is unaffected: the bound is per lane, not global.
	if !q.Submit("g2", func() {}) {
		t.Fatal("one guild filling its lane refused another guild's command")
	}
}

// The cap is global: it is what stops every guild in a busy process running a
// command at the same moment, and it is the only thing COMMAND_PARALLELISM
// sets.
func TestTheCapLimitsCommandsAcrossEveryLane(t *testing.T) {
	q := New(zerolog.Nop(), 2)
	defer q.Close(timeoutCtx(t, time.Second))

	var live, peak atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		lane := string(rune('a' + i))
		q.Submit(lane, func() {
			defer wg.Done()
			if err := q.Acquire(context.Background()); err != nil {
				t.Errorf("acquire: %v", err)
				return
			}
			defer q.Release()
			n := live.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(2 * time.Millisecond)
			live.Add(-1)
		})
	}
	wg.Wait()

	if got := peak.Load(); got > 2 {
		t.Fatalf("%d commands ran at once under a cap of 2", got)
	}
}

// An uncapped queue must not make Acquire a no-op that blocks, or a
// parallelism of zero would stop the bot rather than unleash it.
func TestAnUncappedQueueNeverWaits(t *testing.T) {
	q := New(zerolog.Nop(), 0)
	defer q.Close(timeoutCtx(t, time.Second))

	for i := 0; i < 100; i++ {
		if err := q.Acquire(context.Background()); err != nil {
			t.Fatalf("acquire %d on an uncapped queue: %v", i, err)
		}
	}
	q.Release()
}
