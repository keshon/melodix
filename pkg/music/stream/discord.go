package stream

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"

	"github.com/bwmarrin/discordgo"
	"github.com/godeps/opus"
)

// safeOpusSend sends a packet to vc.OpusSend. Returns false if the channel was closed (voice connection gone).
func safeOpusSend(vc *discordgo.VoiceConnection, packet []byte) (sent bool) {
	defer func() { _ = recover() }()
	vc.OpusSend <- packet
	return true
}

// StreamToDiscord streams audio from a reader to a voice connection.
// The caller owns the stream and must close it when done; StreamToDiscord does not close it.
func StreamToDiscord(stream io.ReadCloser, stop <-chan struct{}, vc *discordgo.VoiceConnection) error {
	encoder, err := opus.NewEncoder(SampleRate, Channels, opus.AppAudio)
	if err != nil {
		return fmt.Errorf("encoder error: %w", err)
	}
	defer encoder.Reset()

	pcmBuf := make([]byte, FrameSize*Channels*2)
	intBuf := make([]int16, FrameSize*Channels)
	opusBuf := make([]byte, 4096)
	const debugPacketCount = 5
	packetNum := 0

	// Warm-up: ffmpeg often outputs silence at pipe start while buffering. Discard
	// a few frames so the first frames we send are real audio (fixes 3-byte OPUS = no sound).
	const warmUpFrames = 10 // 200ms at 20ms/frame
	for i := 0; i < warmUpFrames; i++ {
		select {
		case <-stop:
			return nil
		default:
		}
		_, err := io.ReadFull(stream, pcmBuf)
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return nil
			}
			return fmt.Errorf("warm-up read error: %w", err)
		}
	}
	log.Printf("[Stream] Warm-up: discarded %d frames", warmUpFrames)

	// Skip until we see non-silence (or give up after 3s) so we don't send silent OPUS.
	const silenceThreshold = 100   // min peak to consider "real" audio
	const maxSilenceFrames = 150    // 3s at 20ms/frame
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
			return nil
		default:
		}
		_, err := io.ReadFull(stream, pcmBuf)
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
			log.Printf("[Stream] First non-silence at frame %d (peak >= %d)", skipCount+1, silenceThreshold)
			break
		}
		if skipCount == maxSilenceFrames-1 {
			log.Printf("[Stream] No non-silence after %d frames (~3s); starting anyway (check ffmpeg stderr)", maxSilenceFrames)
		}
	}

	// Encode and send the frame we have (first non-silent or last after give-up).
	log.Printf("[Stream] First send frame max amplitude=%d", frameMaxAbs(intBuf))
	n, err := encoder.Encode(intBuf, opusBuf)
	if err != nil {
		return fmt.Errorf("encode error: %w", err)
	}
	if packetNum < debugPacketCount {
		log.Printf("[Stream] OPUS packet #%d size=%d bytes", packetNum+1, n)
		packetNum++
	}
	select {
	case <-stop:
		return nil
	default:
		if !safeOpusSend(vc, append([]byte(nil), opusBuf[:n]...)) {
			return nil
		}
	}

	for {
		select {
		case <-stop:
			return nil
		default:
			_, err := io.ReadFull(stream, pcmBuf)
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
				log.Printf("[Stream] OPUS packet #%d size=%d bytes", packetNum+1, n)
				packetNum++
			}

			packet := append([]byte(nil), opusBuf[:n]...)
			select {
			case <-stop:
				return nil
			default:
				if !safeOpusSend(vc, packet) {
					return nil
				}
			}
		}
	}
}
