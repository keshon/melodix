package music

import (
	"errors"
	"fmt"
)

// PlayErrorKind is a stable category for /music play failures.
// Command layer should map these to user-facing copy.
type PlayErrorKind string

const (
	PlayErrorKindResolveFailed     PlayErrorKind = "resolve_failed"
	PlayErrorKindNoTracksResolved  PlayErrorKind = "no_tracks_resolved"
	PlayErrorKindEnqueueTrackFailed PlayErrorKind = "enqueue_track_failed"
)

// PlayError provides stable classification for failures in Service.Play.
// It intentionally preserves the underlying error for logging/diagnostics via Unwrap().
type PlayError struct {
	Kind PlayErrorKind
	Err  error
}

func (e *PlayError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Err == nil {
		return fmt.Sprintf("play: %s", e.Kind)
	}
	return fmt.Sprintf("play: %s: %v", e.Kind, e.Err)
}

func (e *PlayError) Unwrap() error { return e.Err }

func (e *PlayError) Is(target error) bool {
	t, ok := target.(*PlayError)
	if !ok {
		return false
	}
	return t.Kind != "" && e.Kind == t.Kind
}

var (
	// ErrPlayResolveFailed indicates the resolver returned an error.
	ErrPlayResolveFailed = &PlayError{Kind: PlayErrorKindResolveFailed}
	// ErrPlayNoTracksResolved indicates the resolver returned zero tracks.
	ErrPlayNoTracksResolved = &PlayError{Kind: PlayErrorKindNoTracksResolved}
	// ErrPlayEnqueueTrackFailed indicates a resolved track could not be enqueued into the player.
	ErrPlayEnqueueTrackFailed = &PlayError{Kind: PlayErrorKindEnqueueTrackFailed}
)

func newPlayError(kind PlayErrorKind, err error) error {
	if err == nil {
		return &PlayError{Kind: kind}
	}
	// Preserve joins/wrapped errors for errors.Is / errors.As.
	if errors.Is(err, &PlayError{Kind: kind}) {
		return err
	}
	return &PlayError{Kind: kind, Err: err}
}

