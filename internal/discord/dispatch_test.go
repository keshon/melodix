package discord

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/discord/cmdqueue"
	"github.com/keshon/melodix/internal/discord/execguard"
)

func newDispatchBot(t *testing.T, parallelism int) *Bot {
	t.Helper()
	b := &Bot{
		cfg:      &config.Config{CommandParallelism: parallelism},
		log:      zerolog.Nop(),
		commands: cmdqueue.New(zerolog.Nop()),
	}
	b.setSessionContext(context.Background())
	b.setGuard(execguard.New(parallelism))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		b.commands.Close(ctx)
	})
	return b
}

func inGuild(id string) cmdadapter.Invoker {
	return cmdadapter.Invoker{GuildID: id, ChannelID: "c" + id}
}

// The whole point: the gateway read goroutine hands the body off and goes back
// to reading. Anything it waits for is time the socket is not being read, and
// an interaction that arrives during it is answered past its deadline.
func TestDispatchDoesNotRunTheCommandOnTheCallersGoroutine(t *testing.T) {
	b := newDispatchBot(t, 4)

	release := make(chan struct{})
	running := make(chan struct{})
	b.dispatchInteraction(inGuild("g1"), nil, "slash", "slow", func(context.Context) error {
		close(running)
		<-release
		return nil
	})

	select {
	case <-running:
	case <-time.After(2 * time.Second):
		t.Fatal("the command never started")
	}
	// Reaching here at all is the assertion: dispatchInteraction returned
	// while the command it dispatched is still blocked.
	close(release)
}

// Two guilds must overlap, or the bot is still serial and the parallelism
// setting is still describing something that is not true.
func TestTwoGuildsRunAtTheSameTime(t *testing.T) {
	b := newDispatchBot(t, 4)

	both := make(chan struct{})
	var once sync.Once
	var arrived atomic.Int64
	wait := func(context.Context) error {
		if arrived.Add(1) == 2 {
			once.Do(func() { close(both) })
		}
		select {
		case <-both:
			return nil
		case <-time.After(2 * time.Second):
			t.Error("a guild waited behind another guild's command")
			return nil
		}
	}

	b.dispatchInteraction(inGuild("g1"), nil, "slash", "a", wait)
	b.dispatchInteraction(inGuild("g2"), nil, "slash", "b", wait)

	select {
	case <-both:
	case <-time.After(3 * time.Second):
		t.Fatal("two guilds never ran at once")
	}
}

// One guild's commands stay in order and never overlap: /play and /next
// racing over one queue and one voice connection is not something a user can
// ask for.
func TestOneGuildStaysSequential(t *testing.T) {
	b := newDispatchBot(t, 8)

	var live, peak atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		b.dispatchInteraction(inGuild("g1"), nil, "slash", "c", func(context.Context) error {
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
			return nil
		})
	}
	wg.Wait()

	if got := peak.Load(); got != 1 {
		t.Fatalf("%d of one guild's commands ran at once", got)
	}
}

// COMMAND_PARALLELISM=1 has to mean something now, including across guilds.
// Before the bodies left the gateway goroutine the semaphore could never
// contend, so the setting was inert at every value.
func TestParallelismOfOneSerializesAcrossGuilds(t *testing.T) {
	b := newDispatchBot(t, 1)

	var live, peak atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		guild := inGuild(string(rune('a' + i)))
		b.dispatchInteraction(guild, nil, "slash", "c", func(context.Context) error {
			defer wg.Done()
			n := live.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(2 * time.Millisecond)
			live.Add(-1)
			return nil
		})
	}
	wg.Wait()

	if got := peak.Load(); got != 1 {
		t.Fatalf("%d commands ran at once under a parallelism of 1", got)
	}
}

// Direct messages have no guild. Laning them all together would make one slow
// DM every other DM's wait, which is the head-of-line blocking this change
// exists to remove.
func TestDirectMessagesDoNotShareOneLane(t *testing.T) {
	b := newDispatchBot(t, 4)

	blocked := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	b.dispatchInteraction(cmdadapter.Invoker{ChannelID: "dm1"}, nil, "slash", "slow",
		func(context.Context) error {
			close(blocked)
			<-release
			return nil
		})
	<-blocked

	ran := make(chan struct{})
	b.dispatchInteraction(cmdadapter.Invoker{ChannelID: "dm2"}, nil, "slash", "quick",
		func(context.Context) error {
			close(ran)
			return nil
		})

	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("one direct message waited behind another")
	}
}

// A command that cannot get a slot is refused rather than queued forever:
// past Discord's acknowledgement deadline it could not answer anyway, and a
// readable refusal beats a silent one.
func TestACommandThatCannotGetASlotIsRefused(t *testing.T) {
	b := newDispatchBot(t, 1)

	release := make(chan struct{})
	holding := make(chan struct{})
	b.dispatchInteraction(inGuild("g1"), nil, "slash", "hog", func(context.Context) error {
		close(holding)
		<-release
		return nil
	})
	<-holding

	busy := make(chan struct{})
	b.runWithCommandContext(commandRunOptions{
		onBusy:  func(error) { close(busy) },
		onError: func(error) { t.Error("reported an error rather than busy") },
	}, func(context.Context) error {
		t.Error("ran without a slot")
		return nil
	})

	select {
	case <-busy:
	case <-time.After(slotWaitBudget + 2*time.Second):
		t.Fatal("a command with no slot waited past its own budget")
	}
	close(release)
}
