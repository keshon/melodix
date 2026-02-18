package cmd

// Middleware wraps a command (e.g. logging, permission check, metrics).
// Adapters can use this same pattern; the wrapped type remains Command.
type Middleware func(Command) Command

// Apply applies middlewares in order; the first in the list is the outermost.
func Apply(c Command, mws ...Middleware) Command {
	for _, mw := range mws {
		c = mw(c)
	}
	return c
}
