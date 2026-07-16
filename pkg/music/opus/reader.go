package opus

import (
	"encoding/binary"
	"io"

	gopus "github.com/godeps/opus"
)

// Reader yields consecutive Opus packets (each a 20ms frame on the encode and
// passthrough paths). ReadPacket returns io.EOF at the end of the stream; any
// other error is a real failure (surfaced to recovery).
type Reader interface {
	ReadPacket() ([]byte, error)
	io.Closer
}

// maxDecodedSamples bounds the decode scratch buffer at the largest legal Opus
// frame (120ms @ 48kHz = 5760 samples/channel), so any packet decodes safely.
const maxDecodedSamples = 5760 * Channels

// DecodeReader adapts a packet Reader into an io.ReadCloser of PCM (s16le,
// 48kHz stereo) for the speaker sink. One reused decoder; single-consumer.
func DecodeReader(r Reader) io.ReadCloser {
	dec, err := gopus.NewDecoder(SampleRate, Channels)
	return &decodeReader{r: r, dec: dec, initErr: err, pcm: make([]int16, maxDecodedSamples)}
}

type decodeReader struct {
	r       Reader
	dec     *gopus.Decoder
	initErr error
	pcm     []int16
	buf     []byte // leftover PCM bytes from the last decoded packet
}

func (d *decodeReader) Read(p []byte) (int, error) {
	if d.initErr != nil {
		return 0, d.initErr
	}
	for len(d.buf) == 0 {
		pkt, err := d.r.ReadPacket()
		if err != nil {
			return 0, err
		}
		n, err := d.dec.Decode(pkt, d.pcm)
		if err != nil {
			return 0, err
		}
		d.buf = pcmToBytes(d.pcm[:n*Channels])
	}
	m := copy(p, d.buf)
	d.buf = d.buf[m:]
	return m, nil
}

func (d *decodeReader) Close() error { return d.r.Close() }

func pcmToBytes(pcm []int16) []byte {
	b := make([]byte, len(pcm)*2)
	for i, s := range pcm {
		binary.LittleEndian.PutUint16(b[i*2:], uint16(s))
	}
	return b
}

// Prepend returns a Reader that yields first before delegating to r. Passthrough
// sources use it to peek and validate the first packet, then put it back.
func Prepend(first []byte, r Reader) Reader {
	return &prependReader{first: first, r: r}
}

type prependReader struct {
	first []byte
	r     Reader
}

func (p *prependReader) ReadPacket() ([]byte, error) {
	if p.first != nil {
		pkt := p.first
		p.first = nil
		return pkt, nil
	}
	return p.r.ReadPacket()
}

func (p *prependReader) Close() error { return p.r.Close() }
