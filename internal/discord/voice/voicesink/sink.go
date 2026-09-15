// Package voicesink carries a track's Opus packets to a Discord voice
// channel: join, hand back a sink that feeds the connection, leave again.
//
// It is the Discord-shaped half of pkg/music/sink, whose Provider and
// AudioSink it implements, and it is named for the difference because both
// used to be called sink -- two packages, two Providers, and every file
// importing both had to alias one of them to say which it meant.
//
// End-to-end encryption comes from dave-go, which is pure Go; see
// DaveRegistry for why that matters and for how a connection's session is
// caught on the way past.
package voicesink

import (
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/snowflake/v2"
	"github.com/keshon/melodix/pkg/music/opus"
	"github.com/keshon/melodix/pkg/music/stream"
	"github.com/rs/zerolog"
)

// daveGate is the part of a guild's DAVE session the send path consults:
// whether this frame may go out, or must be held until the MLS group has an
// epoch to encrypt it under. *davesession.Session satisfies it; a nil gate
// means no session was built, which is a channel without E2EE.
type daveGate interface {
	ShouldHoldFrames() bool
}

// Sink forwards a track's Opus packets to a voice connection.
//
// The audio sender owns the 20ms clock and pulls frames rather than being
// pushed packets over a channel, so Stream hands the reader to a frame
// provider and blocks on its result rather than running the loop itself. The
// vendored fork this replaced worked the other way round, which is why the
// warm-up and dead-air handling lives in the provider here.
type Sink struct {
	conn    voice.Conn
	manager voice.Manager
	guildID snowflake.ID
	dave    daveGate
	log     zerolog.Logger
}

// Stream feeds the track to the connection and returns when it ends, when stop
// is closed, or when the transport underneath dies.
//
// That last case is why this does more than wait. disgo's contract offers one
// signal for a connection going away under a running track --
// OpusFrameProvider.Close -- and v0.19.6 never calls it: closing an audio
// sender cancels its goroutine and nothing else, and closing a connection does
// not touch the sender at all. So a bot that was kicked, moved, or whose voice
// websocket dropped stopped being asked for frames and was never told, and
// this blocked forever: the track never ended, the queue never advanced, and
// the status message said "Now Playing" until someone typed /stop.
//
// The two questions below are what melodix can ask instead, and between them
// they cover every way the connection dies: the sender has stopped pulling, or
// the connection this sink was built on is no longer the guild's.
func (d *Sink) Stream(r opus.Reader, stop <-chan struct{}) error {
	provider := &frameProvider{
		r:          r,
		stop:       stop,
		dave:       d.dave,
		holdBudget: daveReadyTimeout,
		now:        time.Now,
		done:       make(chan error, 1),
	}
	provider.markPull()

	d.conn.SetOpusFrameProvider(provider)
	// Dropping the provider releases the reader with it. The sender goroutine
	// disgo starts in its place idles on a nil provider until the connection
	// closes, which is disgo's own lifecycle and not ours to shorten.
	defer d.conn.SetOpusFrameProvider(nil)

	watch := time.NewTicker(transportCheckInterval)
	defer watch.Stop()

	for {
		select {
		case err := <-provider.done:
			return err
		case <-stop:
			return stream.ErrPlaybackStopped
		case <-watch.C:
			if reason := d.transportDead(provider); reason != "" {
				// Ends the provider too, so a sender still holding it cannot
				// keep draining the track into a socket nobody is listening to.
				provider.finish(stream.ErrVoiceTransport)
				d.log.Warn().
					Str("guild_id", d.guildID.String()).
					Str("reason", reason).
					Msg("voice_transport_lost")
				return stream.ErrVoiceTransport
			}
		}
	}
}

// transportDead names how the connection died, or "" while it is alive.
func (d *Sink) transportDead(provider *frameProvider) string {
	// The manager is the only authority on whether a connection is still the
	// guild's: disgo removes one on a voice websocket close it cannot resume
	// from, and on closing the client, neither of which it reports. Compared
	// by identity rather than by reading the connection's own channel field,
	// which the gateway goroutine writes without a lock.
	if d.manager != nil && d.manager.GetConn(d.guildID) != d.conn {
		return "conn_no_longer_registered"
	}

	// A live sender asks for a frame every 20ms. Silence past the budget means
	// the sender goroutine is gone -- which is what a UDP write failing with a
	// closed socket does to it, quietly. A pull that is simply taking a long
	// time is the sender waiting on us, not the transport dying, so a pull in
	// flight counts as alive however long it runs.
	if provider.pulling.Load() {
		return ""
	}
	if time.Since(provider.lastPullAt()) > transportSilence {
		return "sender_stopped_pulling"
	}
	return ""
}

// frameProvider feeds an opus.Reader to disgo's audio sender and reports
// how the track ended back to Stream.
//
// Every exit returns io.EOF rather than the real error. disgo's sender logs
// anything else and carries on calling, so an error returned here would be
// reported once per 20ms for as long as the connection lived; io.EOF is the
// one value it reads as "nothing to send", after which it sends its silence
// frames and stops speaking. The real error goes to Stream over done instead.
//
// Every field but done, ended, pulling and lastPull is owned by the sender
// goroutine, which is the only caller of ProvideOpusFrame and calls it one
// frame at a time. Those four are how Stream watches it from outside.
type frameProvider struct {
	r    opus.Reader
	stop <-chan struct{}

	// dave gates the send path; see holdExpired. nil means no encryption is
	// expected on this channel, so nothing is ever held.
	dave       daveGate
	holdBudget time.Duration
	holdSince  time.Time
	now        func() time.Time

	done   chan error
	once   sync.Once
	primed bool

	// ended stops a sender that has not noticed yet from consuming any more of
	// the track. Without it, a provider Stream has already given up on keeps
	// being asked for frames until disgo gets round to dropping it, and every
	// one of those is a packet the next attempt will not play.
	ended atomic.Bool
	// pulling is true while the sender is inside ProvideOpusFrame, and
	// lastPull is when it was last there. Between them they say whether the
	// sender is alive, without mistaking a slow read for a dead socket.
	pulling  atomic.Bool
	lastPull atomic.Int64
}

func (p *frameProvider) ProvideOpusFrame() ([]byte, error) {
	p.pulling.Store(true)
	defer func() {
		p.markPull()
		p.pulling.Store(false)
	}()

	if p.ended.Load() {
		return nil, io.EOF
	}
	if stopped(p.stop) {
		p.finish(stream.ErrPlaybackStopped)
		return nil, io.EOF
	}

	// Before the reader is touched: a held frame must not consume a packet,
	// or the hold would be heard as a gap rather than as a pause.
	if p.dave != nil && p.dave.ShouldHoldFrames() {
		if p.holdExpired() {
			p.finish(stream.ErrVoiceTransport)
			return nil, io.EOF
		}
		return nil, nil
	}
	p.holdSince = time.Time{}

	if !p.primed {
		p.primed = true
		first, err := p.prime()
		if err != nil {
			p.finish(endOrErr(err))
			return nil, io.EOF
		}
		if first != nil {
			return first, nil
		}
	}

	packet, err := p.r.ReadPacket()
	if err != nil {
		p.finish(endOrErr(err))
		return nil, io.EOF
	}
	return packet, nil
}

func (p *frameProvider) markPull()             { p.lastPull.Store(time.Now().UnixNano()) }
func (p *frameProvider) lastPullAt() time.Time { return time.Unix(0, p.lastPull.Load()) }

// holdExpired starts the hold clock on the first withheld frame and reports
// whether the hold has outlasted its budget.
//
// A frame is withheld because the guild's MLS group has no epoch to encrypt
// it under. dave-go's Encrypt forwards a frame unmodified rather than failing
// when no ratchet is selectable, and disgo's send path never asks, so a
// provider that does not gate here puts frames with no end-to-end layer on a
// channel Discord has marked encrypted -- which receivers expecting E2EE drop,
// and which is heard as silence. This is the fork's per-frame hold, restored
// at the one point on the disgo send path that melodix owns.
//
// The budget exists because a hold nobody ends is worse than a track that
// does: silence with a live "Now Playing" is the wedge this whole layer is
// meant not to have. Expiring reports transport failure, which the player's
// recovery already knows how to rejoin from.
func (p *frameProvider) holdExpired() bool {
	now := p.now()
	if p.holdSince.IsZero() {
		p.holdSince = now
		return false
	}
	return now.Sub(p.holdSince) > p.holdBudget
}

// prime drains the leading packets and returns the first audible one, or nil
// if the track is silent for longer than maxSilenceFrames. Same budget and
// same reasoning as streamToDiscord; see the constants' comments there.
func (p *frameProvider) prime() ([]byte, error) {
	for i := 0; i < warmUpFrames; i++ {
		if _, err := p.r.ReadPacket(); err != nil {
			return nil, err
		}
	}
	for skip := 0; skip < maxSilenceFrames; skip++ {
		packet, err := p.r.ReadPacket()
		if err != nil {
			return nil, err
		}
		if len(packet) >= silenceBytes {
			return packet, nil
		}
	}
	return nil, nil
}

// Close is disgo dropping the provider. v0.19.6 never calls it, which is why
// Stream watches the transport itself rather than waiting here; it is kept
// because the interface has it and a future version may honour it. Dropping a
// provider under a running track is the transport failing rather than the
// track ending, so that is what it reports.
func (p *frameProvider) Close() {
	p.finish(stream.ErrVoiceTransport)
}

func (p *frameProvider) finish(err error) {
	p.ended.Store(true)
	p.once.Do(func() { p.done <- err })
}

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
