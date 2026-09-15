// Package cmdqueue runs command bodies off the gateway read goroutine, in
// arrival order, one guild at a time.
//
// disgo dispatches events synchronously by default: one goroutine reads the
// socket and calls every listener inline, holding the event manager's lock
// while it does. So a command that resolves a hundred-item playlist, or opens
// a stream, held the socket unread for as long as it took -- and an
// interaction arriving in that window was answered past Discord's three-second
// acknowledgement deadline, which the user sees as "The application did not
// respond". It also made COMMAND_PARALLELISM describe a property the runtime
// did not have: commands were already serial, so the semaphore never
// contended.
//
// The gateway loop stays serial, because its ordering is load-bearing --
// Ready and GuildJoin both synchronise commands and must not interleave. What
// moves off it is the command body.
//
// Per guild rather than per command, because a guild's music is inherently
// sequential: /play and /next at the same instant have no meaningful
// interleaving, and serialising them costs nothing anybody wants. Different
// guilds run at the same time, capped by whatever the caller's own limiter
// allows.
package cmdqueue

import (
	"context"
	"runtime/debug"
	"sync"

	"github.com/rs/zerolog"
)

// maxPerLane bounds how deep one key's lane may get.
//
// A queue with no bound trades a blocked gateway for a backlog, which is not
// obviously better: an interaction is answerable for fifteen minutes and a
// lane is served one command at a time, so past some depth the queue is
// holding work whose right to reply has expired. Refusing at the door is a
// message the caller can read; running forty minutes late is not.
//
// Sized for a person mashing a button rather than for load: nobody types this
// many commands into one guild meaning all of them.
const maxPerLane = 64

// Queue holds one FIFO lane per key, each drained by at most one goroutine.
type Queue struct {
	log zerolog.Logger

	mu     sync.Mutex
	lanes  map[string]*lane
	closed bool

	// running counts the lanes with a goroutine attached, so Close can wait
	// for the work already accepted rather than abandon it mid-command.
	running sync.WaitGroup
}

type lane struct {
	pending []func()
	// draining is true while a goroutine is working this lane. A lane with no
	// goroutine and nothing pending is deleted rather than kept, so a bot in
	// ten thousand guilds holds ten thousand nothings.
	draining bool
}

// New creates an empty queue.
func New(log zerolog.Logger) *Queue {
	return &Queue{
		log:   log.With().Str("component", "cmdqueue").Logger(),
		lanes: make(map[string]*lane),
	}
}

// Submit queues fn to run on key's lane, after everything already queued
// there. It returns immediately; ok is false when the queue is closed or the
// lane is full, and in both cases fn will not run and the caller still owes
// somebody an answer.
func (q *Queue) Submit(key string, fn func()) (ok bool) {
	if fn == nil {
		return false
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return false
	}

	l, found := q.lanes[key]
	if !found {
		l = &lane{}
		q.lanes[key] = l
	}
	if len(l.pending) >= maxPerLane {
		q.log.Warn().Str("lane", key).Int("depth", len(l.pending)).Msg("command_lane_full")
		return false
	}
	l.pending = append(l.pending, fn)
	if !l.draining {
		l.draining = true
		q.running.Add(1)
		go q.drain(key, l)
	}
	return true
}

func (q *Queue) drain(key string, l *lane) {
	defer q.running.Done()
	for {
		q.mu.Lock()
		if len(l.pending) == 0 {
			l.draining = false
			// Only if it is still the lane Submit would find: a Submit that
			// arrives after this goroutine exits has to be able to start a
			// new one.
			if q.lanes[key] == l {
				delete(q.lanes, key)
			}
			q.mu.Unlock()
			return
		}
		fn := l.pending[0]
		l.pending = l.pending[1:]
		q.mu.Unlock()

		q.run(key, fn)
	}
}

// run isolates one command from the rest. A panic used to take the gateway
// goroutine with it, which was the whole bot; now it would take one guild's
// lane and every guild behind it, which is no better -- so it stops here, with
// the stack, because a bot that drops one command is better than a bot that
// drops every guild's music.
func (q *Queue) run(key string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			q.log.Error().
				Str("lane", key).
				Interface("panic", r).
				Str("stack", string(debug.Stack())).
				Msg("command_panicked")
		}
	}()
	fn()
}

// Close stops accepting work and waits for what was accepted to finish, or
// for ctx to expire. It reports whether everything drained.
func (q *Queue) Close(ctx context.Context) bool {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()

	drained := make(chan struct{})
	go func() {
		q.running.Wait()
		close(drained)
	}()

	select {
	case <-drained:
		return true
	case <-ctx.Done():
		q.log.Warn().Int("lanes", q.laneCount()).Msg("command_queue_drain_timeout")
		return false
	}
}

func (q *Queue) laneCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.lanes)
}
