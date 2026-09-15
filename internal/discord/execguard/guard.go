package execguard

import "context"

// Guard is a cap on how many commands run at once, across every guild. It is
// intended to be used from Discord event handlers.
//
// It used to carry a timeout as well, which nothing enforced: the context it
// built reached Adapter.Run and was dropped there, because a command's Run
// takes the invocation data rather than a context, and no engine call takes
// one either. A deadline that cannot cancel anything is not a deadline, so it
// is gone rather than documented.
type Guard struct {
	sem chan struct{}
}

func New(parallelism int) *Guard {
	var sem chan struct{}
	if parallelism > 0 {
		sem = make(chan struct{}, parallelism)
	}
	return &Guard{sem: sem}
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

// Release frees the slot the caller holds. Pair it with a successful Acquire
// and nothing else: every Acquire in this package is followed by a deferred
// Release, and the two are two lines apart.
//
// The non-blocking read is not politeness towards an unpaired Release. It
// cannot tell one apart from a real one -- an unpaired Release frees somebody
// else's slot, silently raising the cap by one for as long as the process
// lives -- and blocking instead would deadlock the caller rather than report
// the bug. What it does buy is that Release on an unconfigured guard, which is
// the state between sessions, is a no-op rather than a hang.
func (g *Guard) Release() {
	if g == nil || g.sem == nil {
		return
	}
	select {
	case <-g.sem:
	default:
	}
}
