package stream

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/bwmarrin/discordgo"
	"github.com/godeps/opus"
)

// StreamToDiscord streams audio from a reader to a voice connection
func StreamToDiscord(stream io.ReadCloser, stop <-chan struct{}, vc *discordgo.VoiceConnection) error {
	defer stream.Close()

	encoder, err := opus.NewEncoder(SampleRate, Channels, opus.AppAudio)
	if err != nil {
		return fmt.Errorf("encoder error: %w", err)
	}
	defer encoder.Reset()

	pcmBuf := make([]byte, FrameSize*Channels*2)
	intBuf := make([]int16, FrameSize*Channels)
	opusBuf := make([]byte, 4096)

	for {
		select {
		case <-stop:
			return nil
		default:
			_, err := io.ReadFull(stream, pcmBuf)
			if err != nil {
				if err == io.EOF {
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

			packet := append([]byte(nil), opusBuf[:n]...)
			select {
			case <-stop:
				return nil
			case vc.OpusSend <- packet:
				// sent
			}
		}
	}
}
