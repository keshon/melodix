package discord

import "errors"

// ErrSessionUnhealthy is returned by RunSession when we intentionally restart
// the gateway session. Callers may use it to apply a faster restart delay than
// for non-transient failures.
var ErrSessionUnhealthy = errors.New("discord session unhealthy")

// errNoConnection is what an audio path reports when it was asked for while
// no session was open.
var errNoConnection = errors.New("no Discord connection")
