package stream

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/bwmarrin/discordgo"
	"layeh.com/gopus"
)

// StreamToDiscord streams audio from a reader to a voice connection
func StreamToDiscord(stream io.ReadCloser, stop <-chan struct{}, vc *discordgo.VoiceConnection) error {
	defer stream.Close()

	encoder, err := gopus.NewEncoder(SampleRate, Channels, gopus.Audio)
	if err != nil {
		return fmt.Errorf("encoder error: %w", err)
	}

	pcmBuf := make([]byte, FrameSize*Channels*2)
	intBuf := make([]int16, FrameSize*Channels)

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

			opus, err := encoder.Encode(intBuf, FrameSize, len(pcmBuf))
			if err != nil {
				return fmt.Errorf("encode error: %w", err)
			}

			vc.OpusSend <- opus
		}
	}
}
