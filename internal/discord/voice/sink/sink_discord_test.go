package sink

import (
	"errors"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/pkg/music/stream"
	"github.com/rs/zerolog"
)

func TestSendOpusStopUnblocksStalledSend(t *testing.T) {
	// Unbuffered OpusSend with no reader: the send can never complete.
	vc := &discordgo.VoiceConnection{OpusSend: make(chan []byte)}
	stop := make(chan struct{})
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(stop)
	}()

	start := time.Now()
	err := sendOpus(zerolog.Nop(), vc, []byte{1}, stop, time.Minute)
	if !errors.Is(err, stream.ErrPlaybackStopped) {
		t.Fatalf("err = %v, want ErrPlaybackStopped", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("stop took %v to unblock the send", elapsed)
	}
}

func TestSendOpusTimeoutIsVoiceTransport(t *testing.T) {
	vc := &discordgo.VoiceConnection{OpusSend: make(chan []byte)}
	err := sendOpus(zerolog.Nop(), vc, []byte{1}, make(chan struct{}), 50*time.Millisecond)
	if !errors.Is(err, stream.ErrVoiceTransport) {
		t.Fatalf("err = %v, want ErrVoiceTransport", err)
	}
}

func TestSendOpusClosedChannelIsVoiceTransport(t *testing.T) {
	ch := make(chan []byte)
	close(ch)
	vc := &discordgo.VoiceConnection{OpusSend: ch}
	err := sendOpus(zerolog.Nop(), vc, []byte{1}, make(chan struct{}), time.Minute)
	if !errors.Is(err, stream.ErrVoiceTransport) {
		t.Fatalf("err = %v, want ErrVoiceTransport", err)
	}
}
