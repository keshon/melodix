package commandkit

// Middleware wraps a command with cross-cutting behavior (e.g. logging, permission
// checks, metrics). The result remains a Command; adapters use the same pattern.
type Middleware func(Command) Command

// Apply wraps c with the given middlewares in order; the first in the list is the outermost.
func Apply(c Command, mws ...Middleware) Command {
	for _, mw := range mws {
		c = mw(c)
	}
	return c
}
