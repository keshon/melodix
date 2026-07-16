package opus

import (
	"errors"
	"io"
)

// ErrNotPassthrough marks a WebM/Opus stream whose framing the Discord sender
// can't forward (not a single 20ms frame). Callers fall back to ffmpeg-encode.
var ErrNotPassthrough = errors.New("opus: stream not passthrough-eligible")

// SeekPackets converts a seek position in seconds to a whole number of 20ms packets.
func SeekPackets(seekSec float64) int {
	if seekSec <= 0 {
		return 0
	}
	return int(seekSec * 1000 / FrameMs)
}

// Passthrough demuxes a WebM/Opus stream into a packet Reader, forwarding its
// Opus packets with no decode/encode. It discards seekPackets leading packets
// (seek) and validates the first remaining packet is a single 20ms frame —
// Discord's sender requires that. On any error it closes body and returns the
// error (ErrNotPassthrough for a framing mismatch); on success the Reader owns body.
func Passthrough(body io.ReadCloser, seekPackets int) (Reader, error) {
	dem := Demux(body)
	for i := 0; i < seekPackets; i++ {
		if _, err := dem.ReadPacket(); err != nil {
			_ = dem.Close()
			return nil, err
		}
	}
	first, err := dem.ReadPacket()
	if err != nil {
		_ = dem.Close()
		return nil, err
	}
	if !IsSingle20ms(first) {
		_ = dem.Close()
		return nil, ErrNotPassthrough
	}
	return Prepend(first, dem), nil
}
