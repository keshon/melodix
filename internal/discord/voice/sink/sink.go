package sink

import (
	"io"
	"sync"
	"time"

	"github.com/disgoorg/disgo/voice"
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
	conn voice.Conn
	dave daveGate
	log  zerolog.Logger
}

func (d *Sink) Stream(r opus.Reader, stop <-chan struct{}) error {
	provider := &frameProvider{
		r:          r,
		stop:       stop,
		dave:       d.dave,
		holdBudget: daveReadyTimeout,
		now:        time.Now,
		done:       make(chan error, 1),
	}

	d.conn.SetOpusFrameProvider(provider)
	// Dropping the provider releases the reader with it. The sender goroutine
	// disgo starts in its place idles on a nil provider until the connection
	// closes, which is disgo's own lifecycle and not ours to shorten.
	defer d.conn.SetOpusFrameProvider(nil)

	select {
	case err := <-provider.done:
		return err
	case <-stop:
		return stream.ErrPlaybackStopped
	}
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
// Every field but done is owned by the sender goroutine, which is the only
// caller of ProvideOpusFrame and calls it one frame at a time.
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
}

func (p *frameProvider) ProvideOpusFrame() ([]byte, error) {
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

// Close is disgo dropping the provider, which happens when the connection goes
// away under a running track. That is the transport failing rather than the
// track ending, so it is reported as such and the player's recovery decides
// what to do. A Close after the track already ended is absorbed by once.
func (p *frameProvider) Close() {
	p.finish(stream.ErrVoiceTransport)
}

func (p *frameProvider) finish(err error) {
	p.once.Do(func() { p.done <- err })
}
