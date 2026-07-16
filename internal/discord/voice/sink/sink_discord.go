package sink

import (
	"errors"
	"io"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/pkg/music/opus"
	"github.com/keshon/melodix/pkg/music/stream"
	"github.com/rs/zerolog"
)

// DiscordSink forwards a track's Opus packets straight to a voice connection —
// no encoding (the packets are already 20ms Opus).
type DiscordSink struct {
	vc  *discordgo.VoiceConnection
	log zerolog.Logger
}

func (d *DiscordSink) Stream(r opus.Reader, stop <-chan struct{}) error {
	return streamToDiscord(d.log, r, stop, d.vc)
}

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

// streamToDiscord forwards 20ms Opus packets to a Discord voice connection until
// the stream ends (io.EOF) or stop is closed. The caller owns r's lifecycle.
func streamToDiscord(appLog zerolog.Logger, r opus.Reader, stop <-chan struct{}, vc *discordgo.VoiceConnection) error {
	for i := 0; i < warmUpFrames; i++ {
		if stopped(stop) {
			return stream.ErrPlaybackStopped
		}
		if _, err := r.ReadPacket(); err != nil {
			return endOrErr(err)
		}
	}

	// Skip leading near-silent packets (dead air), then send the first audible one.
	var first []byte
	for skip := 0; skip < maxSilenceFrames; skip++ {
		if stopped(stop) {
			return stream.ErrPlaybackStopped
		}
		pkt, err := r.ReadPacket()
		if err != nil {
			return endOrErr(err)
		}
		if len(pkt) >= silenceBytes {
			first = pkt
			break
		}
	}
	if first != nil {
		if err := sendOpus(appLog, vc, first, stop, opusSendTimeout); err != nil {
			return err
		}
	}

	for {
		if stopped(stop) {
			return stream.ErrPlaybackStopped
		}
		pkt, err := r.ReadPacket()
		if err != nil {
			return endOrErr(err)
		}
		if err := sendOpus(appLog, vc, pkt, stop, opusSendTimeout); err != nil {
			return err
		}
	}
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

// opusSendTimeout bounds a single Opus packet send. Frame cadence is 20ms, so a send
// blocked this long means the voice connection is stalled, not merely slow.
const opusSendTimeout = 3 * time.Second

// sendOpus sends one packet without blocking forever: it aborts on stop (so Player.Stop
// can always unblock the streaming goroutine) and surfaces a stalled or closed OpusSend
// channel as stream.ErrVoiceTransport, feeding the player's transport recovery.
func sendOpus(log zerolog.Logger, vc *discordgo.VoiceConnection, packet []byte, stop <-chan struct{}, timeout time.Duration) (err error) {
	defer func() {
		if r := recover(); r != nil { // send on OpusSend closed by disconnect
			log.Warn().Interface("panic", r).Msg("opus_send_panic")
			err = stream.ErrVoiceTransport
		}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case vc.OpusSend <- packet:
		return nil
	case <-stop:
		return stream.ErrPlaybackStopped
	case <-timer.C:
		return stream.ErrVoiceTransport
	}
}
