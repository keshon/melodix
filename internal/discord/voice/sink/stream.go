package sink

import (
	"errors"
	"io"
	"time"

	"github.com/keshon/melodix/pkg/music/stream"
)

// How a track's leading packets are handled before the 20ms-paced send begins.
// These describe the audio rather than the library carrying it, which is why
// they outlived the discordgo sink that first needed them.
const (
	// warmUpFrames drains a few leading packets to prime the upstream pipeline
	// (ffmpeg/HTTP) before the 20ms-paced send begins.
	warmUpFrames = 10
	// maxSilenceFrames caps how many leading near-silent packets we skip.
	maxSilenceFrames = 150
	// silenceBytes: Opus encodes silence to a few bytes (VBR), so a packet
	// smaller than this is treated as dead air at the track start and skipped.
	silenceBytes = 20
)

// voiceJoinTimeout limits how long we wait for a voice connection to become
// ready (e.g. no permission = no event).
const voiceJoinTimeout = 15 * time.Second

// daveReadyTimeout bounds the wait for end-to-end encryption to come up on a
// channel that uses it. The MLS exchange is a handful of round trips and
// settles in well under a second; ten is long enough that a slow link is not
// mistaken for a broken handshake.
const daveReadyTimeout = 10 * time.Second

func stopped(stop <-chan struct{}) bool {
	select {
	case <-stop:
		return true
	default:
		return false
	}
}

// endOrErr maps a clean end-of-stream to nil (natural track end) and any other
// error through unchanged (surfaced to the player's recovery).
func endOrErr(err error) error {
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

// Silence the unused-import check when stream is only referenced from the
// sink; keeping the import here documents where ErrPlaybackStopped comes from.
var _ = stream.ErrPlaybackStopped
