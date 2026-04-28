package systemevents

import "sync"

type EventType int

const (
	EventRefreshCommands EventType = iota
)

type Event struct {
	Type    EventType
	GuildID string
	Target  string // "all" or a specific command name
}

// Bus is a small in-process event bus for session-scoped bot events.
// Emit is non-blocking; events may be dropped if the buffer is full.
type Bus struct {
	ch chan Event
	mu sync.RWMutex
}

func New(buffer int) *Bus {
	if buffer <= 0 {
		buffer = 32
	}
	return &Bus{ch: make(chan Event, buffer)}
}

func (b *Bus) Emit(ev Event) {
	if b == nil {
		return
	}
	b.mu.RLock()
	ch := b.ch
	b.mu.RUnlock()
	if ch == nil {
		return
	}
	select {
	case ch <- ev:
	default:
	}
}

func (b *Bus) Events() <-chan Event {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	ch := b.ch
	b.mu.RUnlock()
	return ch
}

