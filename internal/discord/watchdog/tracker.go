package watchdog

import (
	"sync/atomic"
	"time"
)

// Tracker tracks minimal per-session health signals used by watchdogs.
type Tracker struct {
	lastWSNano atomic.Int64
	readyNano  atomic.Int64
}

func NewTracker() *Tracker {
	t := &Tracker{}
	t.lastWSNano.Store(time.Now().UnixNano())
	t.readyNano.Store(0)
	return t
}

func (t *Tracker) MarkWSNow() {
	t.lastWSNano.Store(time.Now().UnixNano())
}

func (t *Tracker) MarkReadyNow() {
	t.readyNano.Store(time.Now().UnixNano())
}

func (t *Tracker) IsReady() bool {
	return t.readyNano.Load() != 0
}

func (t *Tracker) LastWSTime() time.Time {
	return time.Unix(0, t.lastWSNano.Load())
}

func (t *Tracker) SinceLastWS(now time.Time) time.Duration {
	last := t.LastWSTime()
	if now.Before(last) {
		return 0
	}
	return now.Sub(last)
}

