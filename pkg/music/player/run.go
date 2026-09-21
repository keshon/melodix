package player

import (
	"sync"

	"github.com/keshon/melodix/pkg/music/parsers"
)

// runPhase is where a playback run is in its life.
type runPhase int

const (
	// runOpening: the track is being opened; no audio yet.
	runOpening runPhase = iota
	// runPlaying: the stream is open and packets are flowing to the sink.
	runPlaying
	// runEnded: the run finished, failed or was stopped. Terminal.
	runEnded
)

// run is one track's playback, from the moment it starts opening to its
// teardown. Everything that is reset per track -- the stop and done channels,
// the track, whether it is playing, what the UI was told, whether history has
// a row -- lives here rather than on the Player, so a run can only ever change
// its own.
//
// A generation counter used to approximate this: a goroutine scheduled late
// held a number and compared it with the player's before writing fields that
// every run shared. Now it holds its run, and nothing of a newer run's is
// within reach. Whether it is still the run in charge is p.run == r.
//
// stop, done and stopOnce are set at construction and never reassigned. The
// other fields are guarded by the owning Player's mu.
type run struct {
	// stop tells the run's stream loop to stop: a skip, a stop, or the next
	// track starting.
	stop     chan struct{}
	stopOnce sync.Once
	// done is closed once the run is over: when its playback goroutine exits,
	// or when its open fails and no goroutine was started.
	done chan struct{}

	track parsers.Track
	phase runPhase
	// announced is the parser the last emitted status told the UI about,
	// updated to whatever actually plays as each confirmation arrives. The two
	// diverge when a parser opens, is announced, then dies on its first read.
	announced string
	// recorded is true once a history row exists for this run's track.
	recorded bool
}

func newRun(track parsers.Track) *run {
	return &run{
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
		track: track,
	}
}

// signalStop asks the run's stream loop to stop. Safe to call any number of
// times.
func (r *run) signalStop() { r.stopOnce.Do(func() { close(r.stop) }) }

// live reports whether r is opening or playing; a nil run is not. The caller
// holds the player's mu.
func (r *run) live() bool { return r != nil && r.phase != runEnded }
