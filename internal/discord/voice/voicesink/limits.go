package voicesink

import "time"

// The numbers this layer runs on. They are together rather than beside the
// code that reads them because each is a judgement about the network or the
// audio rather than about the logic, and a judgement is easier to revisit when
// it is not buried in a loop.

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

// How often a running track asks whether its transport is still there, and how
// long the audio sender may go without asking for a frame before the answer is
// no.
//
// The sender asks every 20ms, so any silence at all is already abnormal; half
// a second is a margin for a scheduler that lost the goroutine for a moment,
// not a diagnosis. Erring long is cheap -- the cost of noticing late is a
// second of silence -- while erring short would tear down a healthy connection
// on a busy host, which is expensive and self-inflicting.
const (
	transportCheckInterval = 250 * time.Millisecond
	transportSilence       = 500 * time.Millisecond
)

// maxPullStall bounds a single pull: how long the audio sender may sit inside
// one ProvideOpusFrame before the source it is reading counts as dead rather
// than slow.
//
// Everything else here is measured against the sender's 20ms clock, which does
// not apply: a pull blocks in the media pipeline, and a first pull can wait on
// ffmpeg starting up on a cold host. So this is measured against joining
// instead -- the same fifteen seconds voiceJoinTimeout allows, for the same
// reason, that being a length of time nothing healthy takes and a listener
// will already have given up on.
const maxPullStall = 15 * time.Second

// sendReportEvery bounds how often a running track reports what it has put on
// the wire. It is deliberately slow: the tally is for reading a log after the
// fact, not for watching one live, and a line every few seconds per guild is
// how a log stops being read at all.
const sendReportEvery = 30 * time.Second
