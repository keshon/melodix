package soundcloudapi

import (
	"sync/atomic"

	"github.com/rs/zerolog"
)

var logPtr atomic.Pointer[zerolog.Logger]

// SetLogger sets the package logger (client_id rotation diagnostics).
// Safe for concurrent use; call once at process startup.
func SetLogger(l zerolog.Logger) {
	logPtr.Store(&l)
}

func logger() zerolog.Logger {
	if l := logPtr.Load(); l != nil {
		return *l
	}
	return zerolog.Nop()
}
