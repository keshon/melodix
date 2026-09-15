package voicesink

import (
	"context"
	"errors"
	"io"
	"iter"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/snowflake/v2"
	"github.com/rs/zerolog"

	"github.com/keshon/melodix/pkg/music/stream"
)

const testGuild = snowflake.ID(1)

// stubUDP is the socket under a voice connection, with a switch for the two
// ways disgo's sender reacts to a write failing: net.ErrClosed closes the
// sender, and anything else is logged and ignored.
type stubUDP struct {
	writes  atomic.Int64
	failing atomic.Value // error, or nil
}

func (u *stubUDP) Write(p []byte) (int, error) {
	u.writes.Add(1)
	if err, ok := u.failing.Load().(error); ok && err != nil {
		return 0, err
	}
	return len(p), nil
}

func (u *stubUDP) fail(err error) { u.failing.Store(err) }

func (u *stubUDP) Read(p []byte) (int, error) { return 0, io.EOF }
func (u *stubUDP) ReadPacket() (*voice.Packet, error) {
	return nil, io.EOF
}
func (u *stubUDP) Close() error                                    { return nil }
func (u *stubUDP) LocalAddr() net.Addr                             { return nil }
func (u *stubUDP) RemoteAddr() net.Addr                            { return nil }
func (u *stubUDP) SetSecretKey(voice.EncryptionMode, []byte) error { return nil }
func (u *stubUDP) SetDeadline(time.Time) error                     { return nil }
func (u *stubUDP) SetReadDeadline(time.Time) error                 { return nil }
func (u *stubUDP) SetWriteDeadline(time.Time) error                { return nil }
func (u *stubUDP) Open(context.Context, string, int, uint32) (string, int, error) {
	return "", 0, nil
}

// stubConn is a voice connection carrying disgo's real audio sender, so the
// pull model under test is the one that ships rather than an imitation of it.
type stubConn struct {
	udp    *stubUDP
	sender voice.AudioSender
}

func newStubConn() *stubConn {
	c := &stubConn{udp: &stubUDP{}}
	return c
}

func (c *stubConn) SetOpusFrameProvider(provider voice.OpusFrameProvider) {
	if c.sender != nil {
		c.sender.Close()
		c.sender = nil
	}
	if provider == nil {
		return
	}
	c.sender = voice.NewAudioSender(slog.New(slog.DiscardHandler), provider, c)
	c.sender.Open()
}

func (c *stubConn) UDP() voice.UDPConn                                     { return c.udp }
func (c *stubConn) Gateway() voice.Gateway                                 { return nil }
func (c *stubConn) ChannelID() *snowflake.ID                               { return nil }
func (c *stubConn) GuildID() snowflake.ID                                  { return testGuild }
func (c *stubConn) UserIDBySSRC(uint32) snowflake.ID                       { return 0 }
func (c *stubConn) SetSpeaking(context.Context, voice.SpeakingFlags) error { return nil }
func (c *stubConn) SetOpusFrameReceiver(voice.OpusFrameReceiver)           {}
func (c *stubConn) SetEventHandlerFunc(voice.EventHandlerFunc)             {}
func (c *stubConn) Close(context.Context)                                  {}
func (c *stubConn) HandleVoiceStateUpdate(gateway.EventVoiceStateUpdate)   {}
func (c *stubConn) HandleVoiceServerUpdate(gateway.EventVoiceServerUpdate) {}
func (c *stubConn) Open(context.Context, snowflake.ID, bool, bool) error {
	return nil
}

// stubManager answers only the question the sink asks it.
type stubManager struct{ conn atomic.Value }

func (m *stubManager) GetConn(snowflake.ID) voice.Conn {
	c, _ := m.conn.Load().(voice.Conn)
	return c
}
func (m *stubManager) set(c voice.Conn) { m.conn.Store(c) }

func (m *stubManager) CreateConn(snowflake.ID) voice.Conn                     { return nil }
func (m *stubManager) RemoveConn(snowflake.ID)                                {}
func (m *stubManager) Conns() iter.Seq[voice.Conn]                            { return func(func(voice.Conn) bool) {} }
func (m *stubManager) Close(context.Context)                                  {}
func (m *stubManager) HandleVoiceStateUpdate(gateway.EventVoiceStateUpdate)   {}
func (m *stubManager) HandleVoiceServerUpdate(gateway.EventVoiceServerUpdate) {}

// endlessAudio is a track that never ends on its own, so the only thing that
// can end the test is the transport.
type endlessAudio struct{ reads atomic.Int64 }

func (a *endlessAudio) ReadPacket() ([]byte, error) {
	a.reads.Add(1)
	return make([]byte, silenceBytes+1), nil
}
func (a *endlessAudio) Close() error { return nil }

func newTestSink(conn voice.Conn, manager voice.Manager) *Sink {
	return &Sink{conn: conn, manager: manager, guildID: testGuild, log: zerolog.Nop()}
}

// The proven wedge: the socket is closed under a running track, disgo's sender
// sees net.ErrClosed, closes itself, and never calls the provider again. It
// never calls Close on the provider either, so before this was watched for,
// Stream blocked here forever -- the track never ended and the queue never
// advanced.
func TestClosedSocketEndsTheTrackAsTransportFailure(t *testing.T) {
	conn := newStubConn()
	manager := &stubManager{}
	manager.set(conn)

	audio := &endlessAudio{}
	done := make(chan error, 1)
	go func() { done <- newTestSink(conn, manager).Stream(audio, make(chan struct{})) }()

	waitUntil(t, 2*time.Second, func() bool { return conn.udp.writes.Load() > 2 })
	conn.udp.fail(net.ErrClosed)

	select {
	case err := <-done:
		if !errors.Is(err, stream.ErrVoiceTransport) {
			t.Fatalf("want ErrVoiceTransport, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Stream never returned: this is the wedge")
	}
}

// The quieter variant: a write error that is not a closed socket is logged by
// disgo and ignored, so the sender keeps pulling at 50Hz and the whole track
// "plays" into a socket carrying nothing. Melodix cannot see that error, so
// this pins what it can see -- that a track whose frames are going nowhere
// still ends the way one that played would.
func TestAGenericWriteErrorStillLetsTheTrackFinish(t *testing.T) {
	conn := newStubConn()
	manager := &stubManager{}
	manager.set(conn)

	audio := &endlessAudio{}
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- newTestSink(conn, manager).Stream(audio, stop) }()

	waitUntil(t, 2*time.Second, func() bool { return conn.udp.writes.Load() > 2 })
	conn.udp.fail(errors.New("something the sender will only log"))

	// The sender keeps pulling, so the sink must not call this a transport
	// failure: nothing melodix can observe has changed.
	time.Sleep(3 * transportSilence)
	select {
	case err := <-done:
		t.Fatalf("a logged write error ended the track: %v", err)
	default:
	}

	close(stop)
	if err := <-done; !errors.Is(err, stream.ErrPlaybackStopped) {
		t.Fatalf("want ErrPlaybackStopped, got %v", err)
	}
}

// Being kicked or moved, or a voice websocket closing on a code disgo cannot
// resume from, all end the same way: disgo deregisters the connection without
// telling anyone. The manager is the only place that shows.
func TestAConnRemovedBehindOurBackEndsTheTrack(t *testing.T) {
	conn := newStubConn()
	manager := &stubManager{}
	manager.set(conn)

	audio := &endlessAudio{}
	done := make(chan error, 1)
	go func() { done <- newTestSink(conn, manager).Stream(audio, make(chan struct{})) }()

	waitUntil(t, 2*time.Second, func() bool { return conn.udp.writes.Load() > 2 })
	manager.set(newStubConn()) // a rejoin, or a removal: either way not ours

	select {
	case err := <-done:
		if !errors.Is(err, stream.ErrVoiceTransport) {
			t.Fatalf("want ErrVoiceTransport, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Stream kept feeding a connection that is no longer the guild's")
	}
}

// A healthy track must survive the watchdog: a check that fires on a live
// connection would cut every track short at half a second.
func TestAHealthyTrackIsNotMistakenForADeadTransport(t *testing.T) {
	conn := newStubConn()
	manager := &stubManager{}
	manager.set(conn)

	audio := &endlessAudio{}
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- newTestSink(conn, manager).Stream(audio, stop) }()

	time.Sleep(6 * transportSilence)
	select {
	case err := <-done:
		t.Fatalf("the watchdog ended a healthy track: %v", err)
	default:
	}

	close(stop)
	if err := <-done; !errors.Is(err, stream.ErrPlaybackStopped) {
		t.Fatalf("want ErrPlaybackStopped, got %v", err)
	}
}

func waitUntil(t *testing.T, within time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition was never met")
}
