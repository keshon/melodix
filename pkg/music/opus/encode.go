package opus

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	gopus "github.com/godeps/opus"
)

// Encode adapts a PCM stream (s16le, 48kHz stereo — e.g. ffmpeg stdout) into an
// Opus packet Reader by encoding one 20ms frame per packet. The encoder is left
// in its default VBR mode, so silent frames stay tiny (the sink's silence-skip
// heuristic depends on that — do not switch to CBR).
func Encode(pcm io.ReadCloser) Reader {
	enc, err := gopus.NewEncoder(SampleRate, Channels, gopus.AppAudio)
	return &encodeReader{
		src:     pcm,
		enc:     enc,
		initErr: err,
		pcmBuf:  make([]byte, PCMFrameBytes),
		intBuf:  make([]int16, FrameSize*Channels),
		out:     make([]byte, 4000),
	}
}

type encodeReader struct {
	src     io.ReadCloser
	enc     *gopus.Encoder
	initErr error
	pcmBuf  []byte
	intBuf  []int16
	out     []byte
}

func (e *encodeReader) ReadPacket() ([]byte, error) {
	if e.initErr != nil {
		return nil, e.initErr
	}
	// A short final frame (ErrUnexpectedEOF) or clean EOF ends the stream;
	// any other error is a real read failure (e.g. ffmpeg died) and propagates.
	if _, err := io.ReadFull(e.src, e.pcmBuf); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, io.EOF
		}
		return nil, err
	}
	for i := range e.intBuf {
		e.intBuf[i] = int16(binary.LittleEndian.Uint16(e.pcmBuf[i*2:]))
	}
	n, err := e.enc.Encode(e.intBuf, e.out)
	if err != nil {
		return nil, fmt.Errorf("opus: encode: %w", err)
	}
	pkt := make([]byte, n)
	copy(pkt, e.out[:n])
	return pkt, nil
}

func (e *encodeReader) Close() error { return e.src.Close() }
