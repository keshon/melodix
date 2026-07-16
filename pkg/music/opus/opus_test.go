package opus

import (
	"bytes"
	"errors"
	"io"
	"math"
	"os"
	"testing"

	gopus "github.com/godeps/opus"
)

// TestDemuxRealSample cross-checks the demuxer against a real YouTube WebM/Opus
// file (its packet count must match `ffprobe -show_packets`). Opt-in:
// MELODIX_WEBM=path/to/sample.webm go test -run RealSample -v ./pkg/music/opus
func TestDemuxRealSample(t *testing.T) {
	path := os.Getenv("MELODIX_WEBM")
	if path == "" {
		t.Skip("set MELODIX_WEBM to a real .webm to cross-check against ffprobe")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	d := Demux(f)
	n, bad := 0, 0
	for {
		p, err := d.ReadPacket()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("ReadPacket at %d: %v", n, err)
		}
		if !IsSingle20ms(p) {
			bad++
		}
		n++
	}
	t.Logf("demuxed %d packets, %d not single-20ms", n, bad)
	if n == 0 {
		t.Fatal("no packets demuxed")
	}
}

// sliceReader serves pre-made packets as an opus.Reader.
type sliceReader struct {
	pkts [][]byte
	i    int
}

func (s *sliceReader) ReadPacket() ([]byte, error) {
	if s.i >= len(s.pkts) {
		return nil, io.EOF
	}
	p := s.pkts[s.i]
	s.i++
	return p, nil
}
func (s *sliceReader) Close() error { return nil }

// encodeFrames returns n real 20ms Opus packets (a quiet sine, to avoid DTX).
func encodeFrames(t *testing.T, n int) [][]byte {
	t.Helper()
	enc, err := gopus.NewEncoder(SampleRate, Channels, gopus.AppAudio)
	if err != nil {
		t.Fatalf("encoder: %v", err)
	}
	pkts := make([][]byte, 0, n)
	pcm := make([]int16, FrameSize*Channels)
	out := make([]byte, 4000)
	phase := 0.0
	for f := 0; f < n; f++ {
		for i := 0; i < FrameSize; i++ {
			v := int16(3000 * math.Sin(phase))
			pcm[i*2] = v
			pcm[i*2+1] = v
			phase += 2 * math.Pi * 440 / SampleRate
		}
		m, err := enc.Encode(pcm, out)
		if err != nil {
			t.Fatalf("encode frame %d: %v", f, err)
		}
		pkts = append(pkts, append([]byte(nil), out[:m]...))
	}
	return pkts
}

// --- minimal WebM muxer (test only) ---

func szvint(n int) []byte {
	for L := 1; L <= 8; L++ {
		if uint64(n) < (uint64(1)<<(7*L))-1 {
			v := uint64(n) | (uint64(1) << (7 * L))
			b := make([]byte, L)
			for i := L - 1; i >= 0; i-- {
				b[i] = byte(v)
				v >>= 8
			}
			return b
		}
	}
	panic("size too big")
}

var unknownSize = []byte{0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

func cat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func elem(id []byte, payload []byte) []byte {
	return cat(id, szvint(len(payload)), payload)
}

// muxWebM builds a minimal WebM/Opus stream (unknown-size Segment & Cluster,
// like YouTube) carrying the given packets on audio track 1.
func muxWebM(pkts [][]byte) []byte {
	trackEntry := elem([]byte{0xAE}, cat(
		elem([]byte{0xD7}, []byte{0x01}),     // TrackNumber = 1
		elem([]byte{0x83}, []byte{0x02}),     // TrackType = audio
		elem([]byte{0x86}, []byte("A_OPUS")), // CodecID
	))
	tracks := elem([]byte{0x16, 0x54, 0xAE, 0x6B}, trackEntry)

	var blocks []byte
	for _, pkt := range pkts {
		body := cat([]byte{0x81, 0x00, 0x00, 0x00}, pkt) // track vint=1, timecode, flags=0
		blocks = append(blocks, cat([]byte{0xA3}, szvint(len(body)), body)...)
	}
	cluster := cat([]byte{0x1F, 0x43, 0xB6, 0x75}, unknownSize, blocks)
	segment := cat([]byte{0x18, 0x53, 0x80, 0x67}, unknownSize, tracks, cluster)
	ebmlHeader := elem([]byte{0x1A, 0x45, 0xDF, 0xA3}, nil) // empty; demuxer skips it
	return cat(ebmlHeader, segment)
}

func TestDemuxRoundTrip(t *testing.T) {
	pkts := encodeFrames(t, 12)
	stream := muxWebM(pkts)

	d := Demux(io.NopCloser(bytes.NewReader(stream)))
	var got [][]byte
	for {
		p, err := d.ReadPacket()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("ReadPacket: %v", err)
		}
		got = append(got, p)
	}
	if len(got) != len(pkts) {
		t.Fatalf("demuxed %d packets, want %d", len(got), len(pkts))
	}
	for i := range got {
		if !bytes.Equal(got[i], pkts[i]) {
			t.Fatalf("packet %d differs after demux", i)
		}
		if !IsSingle20ms(got[i]) {
			t.Fatalf("packet %d is not single-20ms (toc=0x%02x)", i, got[i][0])
		}
	}
}

func TestDemuxTruncatedTail(t *testing.T) {
	pkts := encodeFrames(t, 8)
	stream := muxWebM(pkts)
	truncated := stream[:len(stream)-40] // cut mid last block

	d := Demux(io.NopCloser(bytes.NewReader(truncated)))
	n := 0
	for {
		_, err := d.ReadPacket()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("truncation should end cleanly, got: %v", err)
		}
		n++
	}
	if n < 6 || n > 8 {
		t.Fatalf("got %d packets from truncated stream, want ~7", n)
	}
}

func TestDemuxLacingRejected(t *testing.T) {
	// One SimpleBlock with a lacing flag set (fixed-lace bit).
	body := cat([]byte{0x81, 0x00, 0x00, 0x02}, []byte{0xFC, 0x00, 0x00}) // flags=0x02 → lacing
	block := cat([]byte{0xA3}, szvint(len(body)), body)
	trackEntry := elem([]byte{0xAE}, cat(
		elem([]byte{0xD7}, []byte{0x01}),
		elem([]byte{0x83}, []byte{0x02}),
		elem([]byte{0x86}, []byte("A_OPUS")),
	))
	stream := cat(
		elem([]byte{0x1A, 0x45, 0xDF, 0xA3}, nil),
		[]byte{0x18, 0x53, 0x80, 0x67}, unknownSize,
		elem([]byte{0x16, 0x54, 0xAE, 0x6B}, trackEntry),
		[]byte{0x1F, 0x43, 0xB6, 0x75}, unknownSize, block,
	)
	d := Demux(io.NopCloser(bytes.NewReader(stream)))
	if _, err := d.ReadPacket(); !errors.Is(err, ErrLacing) {
		t.Fatalf("err = %v, want ErrLacing", err)
	}
}

func TestPassthrough(t *testing.T) {
	pkts := encodeFrames(t, 10)

	// No seek: yields every packet, first one intact.
	r, err := Passthrough(io.NopCloser(bytes.NewReader(muxWebM(pkts))), 0)
	if err != nil {
		t.Fatalf("Passthrough: %v", err)
	}
	first, err := r.ReadPacket()
	if err != nil || !bytes.Equal(first, pkts[0]) {
		t.Fatalf("first packet mismatch (err=%v)", err)
	}
	n := 1
	for {
		if _, err := r.ReadPacket(); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatalf("read: %v", err)
		}
		n++
	}
	if n != len(pkts) {
		t.Fatalf("got %d packets, want %d", n, len(pkts))
	}

	// Seek: discard 3 packets → stream starts at packet index 3.
	r2, err := Passthrough(io.NopCloser(bytes.NewReader(muxWebM(pkts))), 3)
	if err != nil {
		t.Fatalf("Passthrough seek: %v", err)
	}
	defer r2.Close()
	f2, err := r2.ReadPacket()
	if err != nil || !bytes.Equal(f2, pkts[3]) {
		t.Fatalf("seek should start at packet 3 (err=%v)", err)
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	// 10 frames of PCM silence → Encode → packets → DecodeReader → PCM.
	frames := 10
	pcm := make([]byte, frames*PCMFrameBytes)
	enc := Encode(io.NopCloser(bytes.NewReader(pcm)))
	var pkts [][]byte
	for {
		p, err := enc.ReadPacket()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		pkts = append(pkts, p)
	}
	if len(pkts) != frames {
		t.Fatalf("encoded %d packets, want %d", len(pkts), frames)
	}

	out, err := io.ReadAll(DecodeReader(&sliceReader{pkts: pkts}))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if want := frames * PCMFrameBytes; len(out) != want {
		t.Fatalf("decoded %d PCM bytes, want %d", len(out), want)
	}
}

func TestFramingHelpers(t *testing.T) {
	cases := []struct {
		name   string
		toc    byte
		second byte // for frame-count code 3
		single bool
		durMs  float64
	}{
		{"CELT-FB 20ms single", 31 << 3, 0, true, 20},  // config 31, code 0
		{"SILK-NB 40ms single", 2 << 3, 0, false, 40},  // config 2 = 40ms
		{"CELT-FB 20ms x2", 31<<3 | 1, 0, false, 40},   // code 1 → 2 frames
		{"CELT-FB 10ms single", 30 << 3, 0, false, 10}, // config 30 = 10ms
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pkt := []byte{tc.toc, tc.second}
			if got := IsSingle20ms(pkt); got != tc.single {
				t.Fatalf("IsSingle20ms = %v, want %v", got, tc.single)
			}
			if got := PacketDurationMs(pkt); got != tc.durMs {
				t.Fatalf("PacketDurationMs = %v, want %v", got, tc.durMs)
			}
		})
	}
}
