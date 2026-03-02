package commandkit

import "context"

// Unwrappable is implemented by wrapped commands so adapters can reach the
// inner command (e.g. to type-assert to SlashProvider or ComponentHandler).
type Unwrappable interface {
	Command
	Unwrap() Command
}

// Wrapped wraps a command with a custom Run function. Used by middleware; the
// inner command is exposed via Unwrap() so adapters can access provider interfaces.
type Wrapped struct {
	Inner   Command
	RunFunc func(ctx context.Context, inv *Invocation) error
}

// Name delegates to the inner command.
func (w *Wrapped) Name() string { return w.Inner.Name() }

// Description delegates to the inner command.
func (w *Wrapped) Description() string { return w.Inner.Description() }

// Run executes the wrapper's RunFunc, or delegates to the inner command if RunFunc is nil.
func (w *Wrapped) Run(ctx context.Context, inv *Invocation) error {
	if w.RunFunc != nil {
		return w.RunFunc(ctx, inv)
	}
	return w.Inner.Run(ctx, inv)
}

// Unwrap returns the inner command.
func (w *Wrapped) Unwrap() Command { return w.Inner }

// Wrap returns a command that executes run instead of c.Run, delegating Name and
// Description to c. Use from middleware; the returned command implements Unwrappable.
func Wrap(c Command, run func(ctx context.Context, inv *Invocation) error) Command {
	return &Wrapped{Inner: c, RunFunc: run}
}

// Root returns the innermost command by repeatedly unwrapping until the command
// does not implement Unwrappable.
func Root(c Command) Command {
	for {
		if u, ok := c.(Unwrappable); ok {
			c = u.Unwrap()
		} else {
			return c
		}
	}
}
