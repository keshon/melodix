package execguard

import (
	"context"
	"time"
)

// Guard provides a shared concurrency limit and (optional) per-call timeout.
// It is intended to be used from Discord event handlers.
type Guard struct {
	timeout time.Duration
	sem     chan struct{}
}

func New(timeout time.Duration, parallelism int) *Guard {
	var sem chan struct{}
	if parallelism > 0 {
		sem = make(chan struct{}, parallelism)
	}
	return &Guard{timeout: timeout, sem: sem}
}

// Context returns a derived context using Guard's timeout (if configured).
func (g *Guard) Context(base context.Context) (context.Context, context.CancelFunc) {
	if base == nil {
		base = context.Background()
	}
	if g == nil || g.timeout <= 0 {
		return base, func() {}
	}
	return context.WithTimeout(base, g.timeout)
}

// Acquire reserves one execution slot (if parallelism is configured).
func (g *Guard) Acquire(ctx context.Context) error {
	if g == nil || g.sem == nil {
		return nil
	}
	select {
	case g.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release frees one execution slot. Safe to call even if no slot is held.
func (g *Guard) Release() {
	if g == nil || g.sem == nil {
		return
	}
	select {
	case <-g.sem:
	default:
	}
}

