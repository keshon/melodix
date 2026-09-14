package sink

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/pkg/music/stream"
	"github.com/rs/zerolog"
)

func TestParseBackend(t *testing.T) {
	cases := map[string]struct {
		want Backend
		ok   bool
	}{
		"discordgo": {BackendDiscordgo, true},
		"disgo":     {BackendDisgo, true},
		"":          {BackendDiscordgo, false},
		"disco":     {BackendDiscordgo, false},
	}
	for in, want := range cases {
		got, ok := ParseBackend(in)
		if got != want.want || ok != want.ok {
			t.Errorf("ParseBackend(%q) = %q, %v; want %q, %v", in, got, ok, want.want, want.ok)
		}
	}
}

func TestNewDisgoSinkProvider_DefaultVoiceReadyDelay(t *testing.T) {
	p := NewDisgoSinkProvider(NewDisgoVoice(zerolog.Nop()), func() *discordgo.Session { return nil }, "g", 0, zerolog.Nop())
	if p.voiceReadyDelay != 500*time.Millisecond {
		t.Fatalf("unexpected default delay: %v", p.voiceReadyDelay)
	}
}

func TestDisgoSinkProviderInvalidateSink_Idempotent(t *testing.T) {
	p := NewDisgoSinkProvider(NewDisgoVoice(zerolog.Nop()), func() *discordgo.Session { return nil }, "guild1", 0, zerolog.Nop())
	p.InvalidateSink()
	p.InvalidateSink()
	if p.conn != nil || p.currentChannelID != "" {
		t.Fatalf("expected cleared state, got conn=%v channel=%q", p.conn, p.currentChannelID)
	}
}

// The bridge translates discordgo's event into disgo's, and a snowflake that
// will not parse has to drop the event rather than travel on as zero: a zero
// guild id matches no connection, and a zero channel id would read as the bot
// having been moved somewhere real.
func TestVoiceStateUpdateRejectsUnparseableIDs(t *testing.T) {
	bad := &discordgo.VoiceStateUpdate{VoiceState: &discordgo.VoiceState{
		GuildID: "not-a-snowflake", UserID: "2", ChannelID: "3",
	}}
	if _, ok := voiceStateUpdate(bad); ok {
		t.Fatal("an unparseable guild id should drop the event")
	}

	nilState := &discordgo.VoiceStateUpdate{}
	if _, ok := voiceStateUpdate(nilState); ok {
		t.Fatal("an event with no voice state should drop")
	}
}

// A leave arrives as an empty channel id and has to reach disgo as a nil
// pointer, which is the only thing its Conn reads as "we are out": anything
// else leaves the connection believing it is still in a channel.
func TestVoiceStateUpdateCarriesLeaveAsNilChannel(t *testing.T) {
	leave := &discordgo.VoiceStateUpdate{VoiceState: &discordgo.VoiceState{
		GuildID: "1", UserID: "2", ChannelID: "", SessionID: "abc",
	}}
	update, ok := voiceStateUpdate(leave)
	if !ok {
		t.Fatal("a leave is a valid event")
	}
	if update.ChannelID != nil {
		t.Fatalf("leave should carry a nil channel, got %v", update.ChannelID)
	}
	if update.SessionID != "abc" {
		t.Fatalf("session id should survive, got %q", update.SessionID)
	}

	join := &discordgo.VoiceStateUpdate{VoiceState: &discordgo.VoiceState{
		GuildID: "1", UserID: "2", ChannelID: "3",
	}}
	update, ok = voiceStateUpdate(join)
	if !ok || update.ChannelID == nil || update.ChannelID.String() != "3" {
		t.Fatalf("join should carry the channel, got %v ok=%v", update.ChannelID, ok)
	}
}

// stubReader plays a fixed list of packets and then returns err.
type stubReader struct {
	packets [][]byte
	err     error
	i       int
}

func (s *stubReader) Close() error { return nil }

func (s *stubReader) ReadPacket() ([]byte, error) {
	if s.i >= len(s.packets) {
		return nil, s.err
	}
	p := s.packets[s.i]
	s.i++
	return p, nil
}

func audible(n int) [][]byte {
	out := make([][]byte, n)
	for i := range out {
		out[i] = make([]byte, silenceBytes+1)
	}
	return out
}

// disgo's sender logs anything but io.EOF and keeps calling, so a provider
// that returned the real error would repeat it every 20ms for the life of the
// connection. The error has to leave over done instead, and every later call
// has to stay quiet.
func TestDisgoFrameProviderReportsEndOverDoneNotToTheSender(t *testing.T) {
	r := &stubReader{packets: audible(warmUpFrames + 2), err: io.EOF}
	p := &disgoFrameProvider{r: r, stop: make(chan struct{}), done: make(chan error, 1)}

	for i := 0; i < 3; i++ {
		if _, err := p.ProvideOpusFrame(); err != nil && !errors.Is(err, io.EOF) {
			t.Fatalf("the sender must only ever see io.EOF, got %v", err)
		}
	}

	select {
	case err := <-p.done:
		if err != nil {
			t.Fatalf("a clean end of stream is not a failure, got %v", err)
		}
	default:
		t.Fatal("the end of the track never reached Stream")
	}
}

// Losing the connection under a running track is the transport failing, which
// the player's recovery is meant to see - not a track that ended.
func TestDisgoFrameProviderCloseReportsTransportFailure(t *testing.T) {
	p := &disgoFrameProvider{r: &stubReader{err: io.EOF}, stop: make(chan struct{}), done: make(chan error, 1)}
	p.Close()

	select {
	case err := <-p.done:
		if !errors.Is(err, stream.ErrVoiceTransport) {
			t.Fatalf("want ErrVoiceTransport, got %v", err)
		}
	default:
		t.Fatal("Close reported nothing")
	}
}

// The first audible packet has to survive the warm-up, or every track loses
// its opening.
func TestDisgoFrameProviderSkipsDeadAirAndKeepsTheFirstAudiblePacket(t *testing.T) {
	packets := audible(warmUpFrames)
	packets = append(packets, make([]byte, silenceBytes-1)) // dead air
	first := []byte("first-audible-packet")
	packets = append(packets, first)

	p := &disgoFrameProvider{
		r:    &stubReader{packets: packets, err: io.EOF},
		stop: make(chan struct{}),
		done: make(chan error, 1),
	}

	got, err := p.ProvideOpusFrame()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != string(first) {
		t.Fatalf("got %q, want the first audible packet %q", got, first)
	}
}
