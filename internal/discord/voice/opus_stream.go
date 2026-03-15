package voice

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"

	"github.com/bwmarrin/discordgo"
	"github.com/godeps/opus"
	"github.com/keshon/melodix/pkg/music/stream"
)

// streamToDiscord streams PCM audio from a reader to a Discord voice connection.
// Uses stream package constants (SampleRate, Channels, FrameSize) for format.
// The caller owns the read closer and must close it when done; streamToDiscord does not close it.
func streamToDiscord(src io.ReadCloser, stop <-chan struct{}, vc *discordgo.VoiceConnection) error {
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
			return nil
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
	log.Printf("[Stream] Warm-up: discarded %d frames", warmUpFrames)

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
			return nil
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
			log.Printf("[Stream] First non-silence at frame %d (peak >= %d)", skipCount+1, silenceThreshold)
			break
		}
		if skipCount == maxSilenceFrames-1 {
			log.Printf("[Stream] No non-silence after %d frames (~3s); starting anyway (check ffmpeg stderr)", maxSilenceFrames)
		}
	}

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

func safeOpusSend(vc *discordgo.VoiceConnection, packet []byte) (sent bool) {
	defer func() { _ = recover() }()
	vc.OpusSend <- packet
	return true
}
