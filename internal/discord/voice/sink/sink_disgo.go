package sink

import (
	"io"
	"sync"

	"github.com/disgoorg/disgo/voice"
	"github.com/keshon/melodix/pkg/music/opus"
	"github.com/keshon/melodix/pkg/music/stream"
	"github.com/rs/zerolog"
)

// DisgoSink forwards a track's Opus packets to a disgo voice connection. It is
// the disgo half of the comparison DiscordSink is the discordgo half of, and
// deliberately keeps the same warm-up and dead-air handling so the only
// difference under test is the library underneath.
//
// The direction of control is inverted from DiscordSink's: disgo's audio
// sender owns the 20ms clock and pulls frames, where discordgo is pushed
// packets over a channel. Stream therefore hands the reader to a provider and
// blocks on its result rather than running the loop itself.
type DisgoSink struct {
	conn voice.Conn
	log  zerolog.Logger
}

func (d *DisgoSink) Stream(r opus.Reader, stop <-chan struct{}) error {
	provider := &disgoFrameProvider{r: r, stop: stop, done: make(chan error, 1)}

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

// disgoFrameProvider feeds an opus.Reader to disgo's audio sender and reports
// how the track ended back to Stream.
//
// Every exit returns io.EOF rather than the real error. disgo's sender logs
// anything else and carries on calling, so an error returned here would be
// reported once per 20ms for as long as the connection lived; io.EOF is the
// one value it reads as "nothing to send", after which it sends its silence
// frames and stops speaking. The real error goes to Stream over done instead.
type disgoFrameProvider struct {
	r    opus.Reader
	stop <-chan struct{}

	done   chan error
	once   sync.Once
	primed bool
}

func (p *disgoFrameProvider) ProvideOpusFrame() ([]byte, error) {
	if stopped(p.stop) {
		p.finish(stream.ErrPlaybackStopped)
		return nil, io.EOF
	}

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

// prime drains the leading packets and returns the first audible one, or nil
// if the track is silent for longer than maxSilenceFrames. Same budget and
// same reasoning as streamToDiscord; see the constants' comments there.
func (p *disgoFrameProvider) prime() ([]byte, error) {
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
func (p *disgoFrameProvider) Close() {
	p.finish(stream.ErrVoiceTransport)
}

func (p *disgoFrameProvider) finish(err error) {
	p.once.Do(func() { p.done <- err })
}
