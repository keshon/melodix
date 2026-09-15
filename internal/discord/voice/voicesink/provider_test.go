package voicesink

import (
	"context"
	"errors"
	"iter"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/snowflake/v2"
	"github.com/rs/zerolog"
)

// joiningManager records what a provider asked it to do, and hands out a fresh
// connection per join the way disgo's does.
type joiningManager struct {
	name    string
	creates atomic.Int64
	removes atomic.Int64

	mu   sync.Mutex
	conn voice.Conn
}

func (m *joiningManager) CreateConn(snowflake.ID) voice.Conn {
	m.creates.Add(1)
	c := newStubConn()
	m.mu.Lock()
	m.conn = c
	m.mu.Unlock()
	return c
}

func (m *joiningManager) GetConn(snowflake.ID) voice.Conn {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.conn
}

func (m *joiningManager) RemoveConn(snowflake.ID) {
	m.removes.Add(1)
	m.drop()
}

// drop is disgo deregistering a connection on its own, which it does on a
// voice websocket close it cannot resume from -- without telling melodix.
func (m *joiningManager) drop() {
	m.mu.Lock()
	m.conn = nil
	m.mu.Unlock()
}

func (m *joiningManager) Conns() iter.Seq[voice.Conn]                            { return func(func(voice.Conn) bool) {} }
func (m *joiningManager) Close(context.Context)                                  {}
func (m *joiningManager) HandleVoiceStateUpdate(gateway.EventVoiceStateUpdate)   {}
func (m *joiningManager) HandleVoiceServerUpdate(gateway.EventVoiceServerUpdate) {}

// session is one gateway session's worth of voice resources, which is what
// dies and is replaced on every reconnect.
type session struct {
	manager *joiningManager
	dave    *DaveRegistry
}

func newSession(name string) *session {
	return &session{manager: &joiningManager{name: name}, dave: NewDaveRegistry()}
}

// swappable is what a Provider is given instead of a session: a way to ask for
// the live one, which may be a different one each time, or none.
type swappable struct{ current atomic.Value }

func (s *swappable) set(sess *session) { s.current.Store(sess) }
func (s *swappable) clear()            { s.current.Store((*session)(nil)) }

func (s *swappable) resolve() (voice.Manager, *DaveRegistry, bool) {
	sess, _ := s.current.Load().(*session)
	if sess == nil {
		return nil, nil, false
	}
	return sess.manager, sess.dave, true
}

func newTestProviderFor(s *swappable) *Provider {
	return NewProvider(s.resolve, testGuild, 1, zerolog.Nop())
}

// A player outlives every gateway session, so the provider it holds does too.
// Holding the session's voice manager meant that after one reconnect the guild
// joined through a manager whose gateway was shut -- which fails immediately,
// forever, and which no sink invalidation could fix because what was stale was
// the thing that makes connections rather than a connection.
func TestAProviderUsesTheLiveSessionAfterARestart(t *testing.T) {
	sessions := &swappable{}
	first := newSession("first")
	sessions.set(first)

	provider := newTestProviderFor(sessions)
	if _, err := provider.Sink("42"); err != nil {
		t.Fatalf("first join: %v", err)
	}
	if got := first.manager.creates.Load(); got != 1 {
		t.Fatalf("first session got %d joins, want 1", got)
	}

	// The gateway drops and comes back: new client, new voice manager, new
	// DAVE registry. The provider and the player are the same objects.
	second := newSession("second")
	sessions.set(second)

	if _, err := provider.Sink("42"); err != nil {
		t.Fatalf("join after restart: %v", err)
	}
	if got := second.manager.creates.Load(); got != 1 {
		t.Fatalf("the restarted session got %d joins, want 1", got)
	}
	if got := first.manager.creates.Load(); got != 1 {
		t.Fatalf("the dead session was used again (%d joins)", got)
	}
}

// Between sessions there is nothing to join with, and saying so is the right
// answer: the player's own recovery retries, and the next session works.
func TestAProviderBetweenSessionsSaysSoAndRecovers(t *testing.T) {
	sessions := &swappable{}
	sessions.clear()

	provider := newTestProviderFor(sessions)
	if _, err := provider.Sink("42"); !errors.Is(err, ErrNoSession) {
		t.Fatalf("want ErrNoSession, got %v", err)
	}

	back := newSession("back")
	sessions.set(back)
	if _, err := provider.Sink("42"); err != nil {
		t.Fatalf("join once a session returned: %v", err)
	}
	if got := back.manager.creates.Load(); got != 1 {
		t.Fatalf("got %d joins, want 1", got)
	}
}

// disgo removes a connection on its own and does not say so, so "are we
// connected" is a question only the manager can answer. Answering it from a
// field of our own handed the next track a sink over a dead socket.
func TestAConnRemovedByTheLibraryForcesARejoin(t *testing.T) {
	sessions := &swappable{}
	live := newSession("live")
	sessions.set(live)

	provider := newTestProviderFor(sessions)
	if _, err := provider.Sink("42"); err != nil {
		t.Fatalf("first join: %v", err)
	}

	// Same channel, still "connected" as far as our own fields know.
	if _, err := provider.Sink("42"); err != nil {
		t.Fatalf("reuse: %v", err)
	}
	if got := live.manager.creates.Load(); got != 1 {
		t.Fatalf("a live connection was not reused (%d joins)", got)
	}

	live.manager.drop()
	if _, err := provider.Sink("42"); err != nil {
		t.Fatalf("join after the conn was dropped: %v", err)
	}
	if got := live.manager.creates.Load(); got != 2 {
		t.Fatalf("the provider reused a connection disgo had removed (%d joins)", got)
	}
}

// The sink is built per acquisition, and so is the DAVE session it gates on:
// a session belongs to one connection, and a rejoin builds a new one.
func TestASinkCarriesTheCurrentSessionsResources(t *testing.T) {
	sessions := &swappable{}
	first := newSession("first")
	sessions.set(first)

	provider := newTestProviderFor(sessions)
	got, err := provider.Sink("42")
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	if got.(*Sink).manager != voice.Manager(first.manager) {
		t.Fatal("the sink was built on a manager other than the live one")
	}

	second := newSession("second")
	sessions.set(second)
	got, err = provider.Sink("42")
	if err != nil {
		t.Fatalf("join after restart: %v", err)
	}
	if got.(*Sink).manager != voice.Manager(second.manager) {
		t.Fatal("the sink kept the dead session's manager")
	}
}
