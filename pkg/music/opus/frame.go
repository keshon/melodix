// Package opus is the audio engine's currency: 20ms Opus packets. It provides a
// packet Reader, a hand-rolled WebM/Opus demuxer (passthrough), and encode/decode
// adapters over the pure-Go godeps/opus (libopus-on-WASM). Everything downstream of
// a parser speaks Opus packets; ffmpeg/PCM is confined to the encode adapter.
package opus

// Canonical audio format constants. Discord voice and this engine both use
// 48kHz stereo, and one Opus packet carries 20ms (960 samples per channel).
const (
	SampleRate = 48000
	Channels   = 2
	FrameSize  = 960 // samples per channel in one 20ms frame
	FrameMs    = 20

	// PCMFrameBytes is one 20ms PCM frame: s16le, stereo → 960*2*2.
	PCMFrameBytes = FrameSize * Channels * 2
)

// tocFrameMs maps an Opus TOC config (0-31) to its frame duration in ms
// (RFC 6716 §3.1). 20ms configs (the ones Discord expects) are the last of
// each group.
var tocFrameMs = [32]float64{
	10, 20, 40, 60, // SILK NB
	10, 20, 40, 60, // SILK MB
	10, 20, 40, 60, // SILK WB
	10, 20, // Hybrid SWB
	10, 20, // Hybrid FB
	2.5, 5, 10, 20, // CELT NB
	2.5, 5, 10, 20, // CELT WB
	2.5, 5, 10, 20, // CELT SWB
	2.5, 5, 10, 20, // CELT FB
}

// FrameCount returns the number of frames packed in an Opus packet, from the
// TOC frame-count code (RFC 6716 §3.1). Returns 0 for an empty packet.
func FrameCount(pkt []byte) int {
	if len(pkt) == 0 {
		return 0
	}
	switch pkt[0] & 0x03 {
	case 0:
		return 1
	case 1, 2:
		return 2
	default: // code 3: arbitrary, count in the next byte
		if len(pkt) < 2 {
			return 0
		}
		return int(pkt[1] & 0x3F)
	}
}

// PacketDurationMs returns the total duration of an Opus packet in ms
// (per-frame duration × frame count), or 0 for an invalid packet.
func PacketDurationMs(pkt []byte) float64 {
	n := FrameCount(pkt)
	if n == 0 {
		return 0
	}
	return tocFrameMs[pkt[0]>>3] * float64(n)
}

// IsSingle20ms reports whether pkt is exactly one 20ms Opus frame — the only
// shape the Discord voice sender (opusSender, fixed 960-sample timestamp step)
// can forward without desync. The encode path always satisfies this; the
// passthrough demuxer must be validated against it.
func IsSingle20ms(pkt []byte) bool {
	return len(pkt) > 0 && pkt[0]&0x03 == 0 && tocFrameMs[pkt[0]>>3] == 20
}
