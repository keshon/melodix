package sink

import (
	"encoding/binary"
	"fmt"
	"io"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/godeps/opus"
	"github.com/keshon/melodix/pkg/music/stream"
	"github.com/rs/zerolog"
)

// DiscordSink implements musicsink.AudioSink by encoding PCM to opus and sending to a voice connection.
type DiscordSink struct {
	vc  *discordgo.VoiceConnection
	log zerolog.Logger
}

func (d *DiscordSink) Stream(src io.ReadCloser, stop <-chan struct{}) error {
	return streamToDiscord(d.log, src, stop, d.vc)
}

// streamToDiscord streams PCM audio from a reader to a Discord voice connection.
// Uses stream package constants (SampleRate, Channels, FrameSize) for format.
// The caller owns the read closer and must close it when done; streamToDiscord does not close it.
func streamToDiscord(appLog zerolog.Logger, src io.ReadCloser, stop <-chan struct{}, vc *discordgo.VoiceConnection) error {
	encoder, err := opus.NewEncoder(stream.SampleRate, stream.Channels, opus.AppAudio)
	if err != nil {
		return fmt.Errorf("encoder error: %w", err)
	}
	defer encoder.Reset()

	pcmBuf := make([]byte, stream.FrameSize*stream.Channels*2)
	intBuf := make([]int16, stream.FrameSize*stream.Channels)
	opusBuf := make([]byte, 4096)
	const debugPacketCount = 5
	packetNum := 0

	const warmUpFrames = 10
	for i := 0; i < warmUpFrames; i++ {
		select {
		case <-stop:
			return stream.ErrPlaybackStopped
		default:
		}
		_, err := io.ReadFull(src, pcmBuf)
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return nil
			}
			return fmt.Errorf("warm-up read error: %w", err)
		}
	}
	appLog.Debug().Int("frames", warmUpFrames).Msg("sink_warmup_done")

	const silenceThreshold = 100
	const maxSilenceFrames = 150
	frameMaxAbs := func(buf []int16) int16 {
		var max int16
		for _, s := range buf {
			if s < 0 {
				s = -s
			}
			if s > max {
				max = s
			}
		}
		return max
	}

	for skipCount := 0; skipCount < maxSilenceFrames; skipCount++ {
		select {
		case <-stop:
			return stream.ErrPlaybackStopped
		default:
		}
		_, err := io.ReadFull(src, pcmBuf)
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return nil
			}
			return fmt.Errorf("skip-silence read error: %w", err)
		}
		for i := range intBuf {
			intBuf[i] = int16(binary.LittleEndian.Uint16(pcmBuf[i*2 : i*2+2]))
		}
		if frameMaxAbs(intBuf) >= silenceThreshold {
			appLog.Debug().Int("frame", skipCount+1).Int("threshold", silenceThreshold).Msg("sink_first_audible")
			break
		}
		if skipCount == maxSilenceFrames-1 {
			appLog.Debug().Int("frames", maxSilenceFrames).Msg("sink_silence_timeout")
		}
	}

	appLog.Debug().Int("max_amplitude", int(frameMaxAbs(intBuf))).Msg("sink_first_amplitude")
	n, err := encoder.Encode(intBuf, opusBuf)
	if err != nil {
		return fmt.Errorf("encode error: %w", err)
	}
	if packetNum < debugPacketCount {
		appLog.Debug().Int("packet", packetNum+1).Int("bytes", n).Msg("sink_opus_packet")
		packetNum++
	}
	if err := sendOpus(appLog, vc, append([]byte(nil), opusBuf[:n]...), stop, opusSendTimeout); err != nil {
		return err
	}

	for {
		select {
		case <-stop:
			return stream.ErrPlaybackStopped
		default:
			_, err := io.ReadFull(src, pcmBuf)
			if err != nil {
				if err == io.EOF || err == io.ErrUnexpectedEOF {
					return nil
				}
				return fmt.Errorf("read error: %w", err)
			}

			for i := range intBuf {
				intBuf[i] = int16(binary.LittleEndian.Uint16(pcmBuf[i*2 : i*2+2]))
			}

			n, err := encoder.Encode(intBuf, opusBuf)
			if err != nil {
				return fmt.Errorf("encode error: %w", err)
			}

			if packetNum < debugPacketCount {
				appLog.Debug().Int("packet", packetNum+1).Int("bytes", n).Msg("sink_opus_packet")
				packetNum++
			}

			packet := append([]byte(nil), opusBuf[:n]...)
			if err := sendOpus(appLog, vc, packet, stop, opusSendTimeout); err != nil {
				return err
			}
		}
	}
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
