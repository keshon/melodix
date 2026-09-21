package discord

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/keshon/command"
	"github.com/rs/zerolog"

	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/discord/adapter"
	"github.com/keshon/melodix/internal/discord/queue"
	"github.com/keshon/melodix/internal/middleware"
)

func newDispatchBot(t *testing.T, parallelism int) *Bot {
	t.Helper()
	b := &Bot{
		cfg:      &config.Config{CommandParallelism: parallelism},
		log:      zerolog.Nop(),
		commands: queue.New(zerolog.Nop(), parallelism),
	}
	b.setSessionContext(context.Background())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		b.commands.Close(ctx)
	})
	return b
}

func inGuild(id string) adapter.Invoker {
	return adapter.Invoker{GuildID: id, ChannelID: "c" + id}
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
	b.dispatchInteraction(adapter.Invoker{ChannelID: "dm1"}, nil, "slash", "slow",
		func(context.Context) error {
			close(blocked)
			<-release
			return nil
		})
	<-blocked

	ran := make(chan struct{})
	b.dispatchInteraction(adapter.Invoker{ChannelID: "dm2"}, nil, "slash", "quick",
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

// recordingResponder keeps the replies a refused command was given.
type recordingResponder struct {
	mu      sync.Mutex
	replies []adapter.Reply
}

func (r *recordingResponder) record(reply adapter.Reply) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.replies = append(r.replies, reply)
	return nil
}

func (r *recordingResponder) AckDeferred(bool) error              { return nil }
func (r *recordingResponder) Respond(reply adapter.Reply) error   { return r.record(reply) }
func (r *recordingResponder) Followup(reply adapter.Reply) error  { return r.record(reply) }
func (r *recordingResponder) EditResponseText(string) error       { return nil }
func (r *recordingResponder) ReplaceMessage(*adapter.Embed) error { return nil }
func (r *recordingResponder) ResolveDeferred() error              { return nil }
func (r *recordingResponder) AnswerEmbedMessage(*adapter.Embed) (string, string, error) {
	return "", "", nil
}

var _ adapter.Responder = (*recordingResponder)(nil)

func (r *recordingResponder) only(t *testing.T) string {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.replies) != 1 || r.replies[0].Embed == nil {
		t.Fatalf("got %d replies, want exactly one embed", len(r.replies))
	}
	return r.replies[0].Embed.Description
}

// A guild with a full lane is busy, not shutting down. It used to be told the
// bot was shutting down, which reads as "stop trying" at the exact moment the
// right answer is "try again shortly".
func TestAFullLaneIsReportedAsBusyNotAsShutdown(t *testing.T) {
	b := newDispatchBot(t, 4)

	release := make(chan struct{})
	defer close(release)
	blocked := make(chan struct{})
	b.dispatchInteraction(inGuild("g1"), nil, "slash", "slow", func(context.Context) error {
		close(blocked)
		<-release
		return nil
	})
	<-blocked
	for i := 0; i < 64; i++ {
		b.dispatchInteraction(inGuild("g1"), nil, "slash", "queued", func(context.Context) error { return nil })
	}

	r := &recordingResponder{}
	b.dispatchInteraction(inGuild("g1"), r, "slash", "refused", func(context.Context) error { return nil })

	if got := r.only(t); strings.Contains(got, "shutting down") {
		t.Fatalf("a full lane replied %q", got)
	}
}

// A closed queue really is shutting down, and still says so.
func TestAClosedQueueStillSaysItIsShuttingDown(t *testing.T) {
	b := newDispatchBot(t, 4)
	b.commands.Close(context.Background())

	r := &recordingResponder{}
	b.dispatchInteraction(inGuild("g1"), r, "slash", "late", func(context.Context) error { return nil })

	if got := r.only(t); !strings.Contains(got, "shutting down") {
		t.Fatalf("a closed queue replied %q", got)
	}
}

// buttonCommand is a registered command with a component handler, like /search.
type buttonCommand struct{ clicks *int }

func (buttonCommand) Name() string                               { return "fake" }
func (buttonCommand) Description() string                        { return "a command with a button" }
func (buttonCommand) Group() string                              { return "music" }
func (buttonCommand) Category() string                           { return "test" }
func (buttonCommand) UserPermissions() []int64                   { return nil }
func (buttonCommand) Run(*adapter.SlashInteractionContext) error { return nil }
func (b buttonCommand) Component(*adapter.ComponentInteractionContext) error {
	*b.clicks++
	return nil
}

type countingAudit struct{ rows int }

func (a *countingAudit) LogCommand(_, _, _, _, _ string) error { a.rows++; return nil }

// A click runs through the middleware the command was registered with. It
// used to be handed straight to the component handler, so no click was ever
// audited and a disabled group's buttons went on working.
func TestAComponentRunsThroughItsCommandsMiddleware(t *testing.T) {
	clicks := 0
	audit := &countingAudit{}
	registered := command.Apply(&adapter.Adapter{Cmd: buttonCommand{clicks: &clicks}},
		middleware.WithGuildOnly(), middleware.WithCommandLogger(zerolog.Nop()))

	err := runComponent(context.Background(), registered, &adapter.ComponentInteractionContext{
		Invoker: inGuild("g1"), ComponentID: "fake:1", Audit: audit, AppLog: zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if clicks != 1 {
		t.Fatalf("handler ran %d times, want 1", clicks)
	}
	if audit.rows != 1 {
		t.Fatalf("audited %d rows for one click, want 1", audit.rows)
	}
}
