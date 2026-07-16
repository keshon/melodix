package opus

import (
	"bufio"
	"errors"
	"fmt"
	"io"
)

// ErrLacing is returned when a WebM SimpleBlock uses lacing (multiple frames per
// block). YouTube's Opus never laces; encountering it means the stream is not
// cleanly passthrough-able, so the caller should fall back to the encode path.
var ErrLacing = errors.New("opus: webm block uses lacing")

// EBML element IDs (length-marker bits retained, matching readVint(keep=true)).
const (
	idEBML         = 0x1A45DFA3
	idSegment      = 0x18538067
	idTracks       = 0x1654AE6B
	idTrackEntry   = 0xAE
	idTrackNumber  = 0xD7
	idTrackType    = 0x83
	idCodecID      = 0x86
	idCodecPrivate = 0x63A2
	idCluster      = 0x1F43B675
	idSimpleBlock  = 0xA3
	idBlock        = 0xA1
)

// Demux parses a WebM/Opus byte stream (e.g. a YouTube CDN response) and yields
// the raw Opus packets — no decode, no ffmpeg. A minimal EBML parser: it
// descends the master wrappers flatly (their children have known sizes) and
// returns each audio-track SimpleBlock payload.
func Demux(src io.ReadCloser) Reader {
	return &demuxer{src: src, r: bufio.NewReaderSize(src, 1<<16)}
}

type demuxer struct {
	src        io.ReadCloser
	r          *bufio.Reader
	audioTrack uint64
	haveTrack  bool
}

func (d *demuxer) Close() error { return d.src.Close() }

func (d *demuxer) ReadPacket() ([]byte, error) {
	for {
		id, _, err := d.readVint(true)
		if err != nil {
			return nil, mapEOF(err)
		}
		size, sLen, err := d.readVint(false)
		if err != nil {
			return nil, mapEOF(err)
		}
		switch id {
		case idSegment, idCluster, idTracks:
			// master wrapper: descend flatly, children follow directly
		case idTrackEntry:
			if err := d.parseTrackEntry(int64(size)); err != nil {
				return nil, mapEOF(err)
			}
		case idSimpleBlock, idBlock:
			data, err := d.readN(int64(size))
			if err != nil {
				return nil, mapEOF(err)
			}
			pkt, err := d.audioPacket(data)
			if err != nil {
				return nil, err
			}
			if pkt != nil {
				return pkt, nil
			}
			// non-audio block: keep scanning
		default:
			if isUnknownSize(size, sLen) {
				continue // unknown-size master: descend flatly
			}
			if err := d.skip(int64(size)); err != nil {
				return nil, mapEOF(err)
			}
		}
	}
}

// audioPacket extracts the Opus packet from a (Simple)Block payload, or returns
// (nil, nil) if the block belongs to another track. ErrLacing if the block laces.
func (d *demuxer) audioPacket(data []byte) ([]byte, error) {
	if len(data) < 4 {
		return nil, nil
	}
	tn, n := vintFromBytes(data)
	if n == 0 || len(data) < n+3 {
		return nil, nil
	}
	if d.haveTrack && tn != d.audioTrack {
		return nil, nil
	}
	flags := data[n+2] // data[n], data[n+1] = int16 timecode
	if lacing := (flags >> 1) & 0x03; lacing != 0 {
		return nil, ErrLacing
	}
	payload := data[n+3:]
	if len(payload) == 0 {
		return nil, nil
	}
	return payload, nil
}

func (d *demuxer) parseTrackEntry(size int64) error {
	remaining := size
	var tn, tt uint64
	var codec string
	for remaining > 0 {
		id, idLen, err := d.readVint(true)
		if err != nil {
			return err
		}
		sz, szLen, err := d.readVint(false)
		if err != nil {
			return err
		}
		data, err := d.readN(int64(sz))
		if err != nil {
			return err
		}
		remaining -= int64(idLen) + int64(szLen) + int64(sz)
		switch id {
		case idTrackNumber:
			tn = beUint(data)
		case idTrackType:
			tt = beUint(data)
		case idCodecID:
			codec = string(data)
		case idCodecPrivate:
			// OpusHead; not needed beyond confirming Opus via CodecID
		}
	}
	if tt == 2 && codec == "A_OPUS" {
		d.audioTrack = tn
		d.haveTrack = true
	}
	return nil
}

// --- EBML primitives (bufio-backed) ---

func (d *demuxer) readVint(keepMarker bool) (val uint64, length int, err error) {
	first, err := d.r.ReadByte()
	if err != nil {
		return 0, 0, err
	}
	mask := byte(0x80)
	length = 1
	for length <= 8 && first&mask == 0 {
		mask >>= 1
		length++
	}
	if length > 8 {
		return 0, 0, fmt.Errorf("opus: bad vint 0x%02x", first)
	}
	if keepMarker {
		val = uint64(first)
	} else {
		val = uint64(first & (mask - 1))
	}
	for i := 1; i < length; i++ {
		b, err := d.r.ReadByte()
		if err != nil {
			return 0, 0, err
		}
		val = (val << 8) | uint64(b)
	}
	return val, length, nil
}

func (d *demuxer) readN(n int64) ([]byte, error) {
	if n < 0 || n > (64<<20) {
		return nil, fmt.Errorf("opus: insane element size %d", n)
	}
	buf := make([]byte, n)
	_, err := io.ReadFull(d.r, buf)
	return buf, err
}

func (d *demuxer) skip(n int64) error {
	_, err := io.CopyN(io.Discard, d.r, n)
	return err
}

func isUnknownSize(size uint64, length int) bool {
	return size == (uint64(1)<<(7*length))-1
}

func beUint(b []byte) uint64 {
	var v uint64
	for _, x := range b {
		v = (v << 8) | uint64(x)
	}
	return v
}

func vintFromBytes(b []byte) (uint64, int) {
	if len(b) == 0 {
		return 0, 0
	}
	first := b[0]
	mask := byte(0x80)
	length := 1
	for length <= 8 && first&mask == 0 {
		mask >>= 1
		length++
	}
	if length > 8 || length > len(b) {
		return 0, 0
	}
	v := uint64(first & (mask - 1))
	for i := 1; i < length; i++ {
		v = (v << 8) | uint64(b[i])
	}
	return v, length
}

// mapEOF turns a truncated-tail read into a clean end-of-stream.
func mapEOF(err error) error {
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return io.EOF
	}
	return err
}
