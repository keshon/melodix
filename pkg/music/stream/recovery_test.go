package stream

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/keshon/melodix/pkg/music/opus"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/sources"
)

type fakeStreamer struct {
	open func(track *parsers.Track, seek float64) (opus.Reader, func(), error)
}

func (s fakeStreamer) Open(track *parsers.Track, seek float64) (opus.Reader, func(), error) {
	return s.open(track, seek)
}

// pktReader yields the given packets, then io.EOF.
type pktReader struct {
	pkts [][]byte
	i    int
}

func (r *pktReader) ReadPacket() ([]byte, error) {
	if r.i >= len(r.pkts) {
		return nil, io.EOF
	}
	p := r.pkts[r.i]
	r.i++
	return p, nil
}
func (r *pktReader) Close() error { return nil }

// errFirst fails immediately on the first ReadPacket (instant fail).
type errFirst struct{}

func (errFirst) ReadPacket() ([]byte, error) { return nil, io.EOF }
func (errFirst) Close() error                { return nil }

func TestRecoveryStream_ImmediateFail_SwitchesToNextParser(t *testing.T) {
	orig := SetRegistry(map[string]parsers.Streamer{
		"p1": fakeStreamer{open: func(*parsers.Track, float64) (opus.Reader, func(), error) {
			return errFirst{}, func() {}, nil
		}},
		"p2": fakeStreamer{open: func(*parsers.Track, float64) (opus.Reader, func(), error) {
			return &pktReader{pkts: [][]byte{{0xAA}}}, func() {}, nil
		}},
	})
	defer func() { SetRegistry(orig) }()

	track := &parsers.Track{SourceInfo: sources.TrackInfo{AvailableParsers: []string{"p1", "p2"}}}
	rs := NewRecoveryStream(track)
	if err := rs.Open(0); err != nil {
		t.Fatalf("Open: %v", err)
	}

	pkt, err := rs.ReadPacket()
	if err != nil {
		t.Fatalf("ReadPacket after recovery: %v", err)
	}
	if len(pkt) != 1 || pkt[0] != 0xAA {
		t.Fatalf("expected p2's packet, got %v", pkt)
	}
	if track.CurrentParser != "p2" {
		t.Fatalf("CurrentParser = %q, want p2", track.CurrentParser)
	}
}

func TestRecoveryStream_NaturalEOF_DoesNotFallback(t *testing.T) {
	orig := SetRegistry(map[string]parsers.Streamer{
		"p1": fakeStreamer{open: func(*parsers.Track, float64) (opus.Reader, func(), error) {
			return &pktReader{pkts: [][]byte{{0xAA}}}, func() {}, nil
		}},
		"p2": fakeStreamer{open: func(*parsers.Track, float64) (opus.Reader, func(), error) {
			return &pktReader{pkts: [][]byte{{0xBB}}}, func() {}, nil
		}},
	})
	defer func() { SetRegistry(orig) }()

	track := &parsers.Track{
		Duration:   1 * time.Microsecond, // tiny → EOF is a natural end
		SourceInfo: sources.TrackInfo{AvailableParsers: []string{"p1", "p2"}},
	}
	rs := NewRecoveryStream(track)
	if err := rs.Open(0); err != nil {
		t.Fatalf("Open: %v", err)
	}

	if _, err := rs.ReadPacket(); err != nil {
		t.Fatalf("first ReadPacket: %v", err)
	}
	if _, err := rs.ReadPacket(); !errors.Is(err, io.EOF) {
		t.Fatalf("second ReadPacket = %v, want EOF (no fallback)", err)
	}
	if track.CurrentParser != "p1" {
		t.Fatalf("stayed off p1: %q", track.CurrentParser)
	}
	if rs.parserIndex != 0 {
		t.Fatalf("parserIndex = %d, want 0", rs.parserIndex)
	}
}

// The failing parser opens fine and only dies on its first read, so Open cannot
// tell it apart from a working one. Confirmation must therefore fire once, for
// the parser that actually produced the packet — that is what the player relies
// on to correct a "Now Playing" embed already naming the dead parser.
func TestRecoveryStream_ParserConfirmed_NamesThePlayingParser(t *testing.T) {
	orig := SetRegistry(map[string]parsers.Streamer{
		"p1": fakeStreamer{open: func(*parsers.Track, float64) (opus.Reader, func(), error) {
			return errFirst{}, func() {}, nil
		}},
		"p2": fakeStreamer{open: func(*parsers.Track, float64) (opus.Reader, func(), error) {
			return &pktReader{pkts: [][]byte{{0xAA}, {0xBB}}}, func() {}, nil
		}},
	})
	defer func() { SetRegistry(orig) }()

	track := &parsers.Track{SourceInfo: sources.TrackInfo{AvailableParsers: []string{"p1", "p2"}}}
	rs := NewRecoveryStream(track)

	var confirmed []string
	rs.SetOnParserConfirmed(func(parser string) { confirmed = append(confirmed, parser) })

	if err := rs.Open(0); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(confirmed) != 0 {
		t.Fatalf("Open must not confirm anything, got %v", confirmed)
	}

	if _, err := rs.ReadPacket(); err != nil {
		t.Fatalf("first ReadPacket: %v", err)
	}
	if len(confirmed) != 1 || confirmed[0] != "p2" {
		t.Fatalf("confirmed = %v, want [p2]", confirmed)
	}

	// Further packets from the same stream must not re-confirm.
	if _, err := rs.ReadPacket(); err != nil {
		t.Fatalf("second ReadPacket: %v", err)
	}
	if len(confirmed) != 1 {
		t.Fatalf("confirmed = %v, want a single confirmation per open", confirmed)
	}
}

// A transport reopen re-confirms, so the parser reported to the player is always
// the live one rather than a stale memory of the first open.
func TestRecoveryStream_ReopenAfterTransportFailure_ReConfirms(t *testing.T) {
	orig := SetRegistry(map[string]parsers.Streamer{
		"p1": fakeStreamer{open: func(*parsers.Track, float64) (opus.Reader, func(), error) {
			return &pktReader{pkts: [][]byte{{0xAA}}}, func() {}, nil
		}},
	})
	defer func() { SetRegistry(orig) }()

	track := &parsers.Track{SourceInfo: sources.TrackInfo{AvailableParsers: []string{"p1"}}}
	rs := NewRecoveryStream(track)

	var confirmed []string
	rs.SetOnParserConfirmed(func(parser string) { confirmed = append(confirmed, parser) })

	if err := rs.Open(0); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := rs.ReadPacket(); err != nil {
		t.Fatalf("first ReadPacket: %v", err)
	}
	if err := rs.ReopenAfterTransportFailure(); err != nil {
		t.Fatalf("ReopenAfterTransportFailure: %v", err)
	}
	if _, err := rs.ReadPacket(); err != nil {
		t.Fatalf("ReadPacket after reopen: %v", err)
	}
	if len(confirmed) != 2 || confirmed[1] != "p1" {
		t.Fatalf("confirmed = %v, want two p1 confirmations", confirmed)
	}
}

// cutReader yields n packets, then fails with err — a stream that dies partway
// rather than ending.
type cutReader struct {
	n   int
	i   int
	err error
}

func (r *cutReader) ReadPacket() ([]byte, error) {
	if r.i >= r.n {
		return nil, r.err
	}
	r.i++
	return []byte{0xAA}, nil
}
func (r *cutReader) Close() error { return nil }

// TestRecoveryStream_MidStreamTransportError_Reopens covers the failure the
// passthrough path actually produces: the CDN drops the connection partway
// through a track, which arrives as a net read error rather than io.EOF. Before
// this was handled, such a track died at the cut instead of resuming.
func TestRecoveryStream_MidStreamTransportError_Reopens(t *testing.T) {
	reset := errors.New("read tcp 10.0.0.1:1->2.2.2.2:443: wsarecv: An existing connection was forcibly closed by the remote host.")

	opens := 0
	var seeks []float64
	orig := SetRegistry(map[string]parsers.Streamer{
		"p1": fakeStreamer{open: func(_ *parsers.Track, seek float64) (opus.Reader, func(), error) {
			opens++
			seeks = append(seeks, seek)
			if opens == 1 {
				// Dies a quarter of the way into a 20s track (1000 packets).
				return &cutReader{n: 250, err: reset}, func() {}, nil
			}
			// The rest of the track, ending naturally.
			return &cutReader{n: 750, err: io.EOF}, func() {}, nil
		}},
	})
	defer func() { SetRegistry(orig) }()

	track := &parsers.Track{
		Duration:   20 * time.Second,
		SourceInfo: sources.TrackInfo{AvailableParsers: []string{"p1"}},
	}
	rs := NewRecoveryStream(track)
	if err := rs.Open(0); err != nil {
		t.Fatalf("Open: %v", err)
	}

	read := 0
	for {
		_, err := rs.ReadPacket()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				t.Fatalf("ended with %v, want EOF after recovery", err)
			}
			break
		}
		read++
	}

	if opens != 2 {
		t.Fatalf("opened %d times, want a reopen after the reset", opens)
	}
	// The reopen resumes where playback got to, not from the start.
	if len(seeks) != 2 || seeks[1] <= 0 {
		t.Fatalf("reopen seeks = %v, want the second to resume mid-track", seeks)
	}
	if read != 1000 {
		t.Fatalf("read %d packets, want the whole 20s track across the cut", read)
	}
}

// A stream torn down by Close must not be resurrected: the reader failing
// because it was closed is not an early end.
func TestRecoveryStream_ClosedStream_DoesNotReopen(t *testing.T) {
	opens := 0
	orig := SetRegistry(map[string]parsers.Streamer{
		"p1": fakeStreamer{open: func(*parsers.Track, float64) (opus.Reader, func(), error) {
			opens++
			return &cutReader{n: 1, err: errors.New("use of closed network connection")}, func() {}, nil
		}},
	})
	defer func() { SetRegistry(orig) }()

	track := &parsers.Track{
		Duration:   20 * time.Second,
		SourceInfo: sources.TrackInfo{AvailableParsers: []string{"p1"}},
	}
	rs := NewRecoveryStream(track)
	if err := rs.Open(0); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := rs.ReadPacket(); err != nil {
		t.Fatalf("first packet: %v", err)
	}
	_ = rs.Close()

	if _, err := rs.ReadPacket(); err == nil {
		t.Fatal("a closed stream must not keep yielding packets")
	}
	if opens != 1 {
		t.Fatalf("opened %d times, want no reopen after Close", opens)
	}
}

// gatedReader yields n packets, then fails; the reopen that follows is held
// until release is closed, so the test can observe what the consumer hears
// while a reconnect is in progress.
type gatedReader struct {
	n   int
	i   int
	err error
}

func (r *gatedReader) ReadPacket() ([]byte, error) {
	if r.i >= r.n {
		return nil, r.err
	}
	r.i++
	return []byte{0xAA}, nil
}
func (r *gatedReader) Close() error { return nil }

// TestRecoveryStream_BufferCoversTheReconnectGap is the point of putting the
// anti-skip buffer above recovery: while the stream underneath is reconnecting,
// the consumer keeps being fed from the queued lead instead of blocking.
//
// Below recovery — where the buffer used to live — a reopen tore it down along
// with the stream it wrapped, and this test could not pass at all: the consumer
// would sit inside ReadPacket until the reopen finished.
func TestRecoveryStream_BufferCoversTheReconnectGap(t *testing.T) {
	SetBufferAhead(4000) // 200 packets of lead
	defer SetBufferAhead(0)

	release := make(chan struct{})
	opens := 0
	orig := SetRegistry(map[string]parsers.Streamer{
		"p1": fakeStreamer{open: func(_ *parsers.Track, _ float64) (opus.Reader, func(), error) {
			opens++
			if opens == 1 {
				return &gatedReader{n: 500, err: errors.New("connection reset by peer")}, func() {}, nil
			}
			<-release // the reconnect takes as long as the test wants
			return &cutReader{n: 500, err: io.EOF}, func() {}, nil
		}},
	})
	defer func() { SetRegistry(orig) }()

	track := &parsers.Track{
		Duration:   20 * time.Second, // 1000 packets
		SourceInfo: sources.TrackInfo{AvailableParsers: []string{"p1"}},
	}
	rs := NewRecoveryStream(track)
	if err := rs.Open(0); err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rs.Close()

	packets := rs.Packets()

	// Drain enough that the producer has run into the cut and is parked in the
	// gated reopen, then keep reading: these come from the buffered lead.
	read := 0
	for read < 400 {
		if _, err := packets.ReadPacket(); err != nil {
			t.Fatalf("packet %d during reconnect: %v", read, err)
		}
		read++
	}

	close(release)

	for {
		_, err := packets.ReadPacket()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				t.Fatalf("ended with %v", err)
			}
			break
		}
		read++
	}
	if read != 1000 {
		t.Fatalf("heard %d packets, want the whole track across the reconnect", read)
	}
	if opens != 2 {
		t.Fatalf("opens = %d", opens)
	}
}

// Packets is cached: a second call must not start a second read-ahead goroutine
// competing for the same source.
func TestRecoveryStream_PacketsIsStable(t *testing.T) {
	SetBufferAhead(1000)
	defer SetBufferAhead(0)

	orig := SetRegistry(map[string]parsers.Streamer{
		"p1": fakeStreamer{open: func(*parsers.Track, float64) (opus.Reader, func(), error) {
			return &cutReader{n: 10, err: io.EOF}, func() {}, nil
		}},
	})
	defer func() { SetRegistry(orig) }()

	rs := NewRecoveryStream(&parsers.Track{
		Duration:   time.Second,
		SourceInfo: sources.TrackInfo{AvailableParsers: []string{"p1"}},
	})
	if err := rs.Open(0); err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rs.Close()

	if rs.Packets() != rs.Packets() {
		t.Fatal("Packets returned two different readers")
	}
}
