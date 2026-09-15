package stream

import (
	"reflect"
	"testing"
	"time"

	"github.com/keshon/melodix/pkg/music/opus"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/sources"
)

// A stream must not write the Track it was handed. Everything below it does
// write one -- parsers fill in Title and Duration through the pointer they get
// at open time, and recovery rewrites CurrentParser as fallbacks engage -- so
// the property is that those writes land on the stream's own copy.
func TestRecoveryStreamDoesNotWriteTheCallersTrack(t *testing.T) {
	orig := SetRegistry(map[string]parsers.Streamer{
		"p1": fakeStreamer{open: func(tr *parsers.Track, _ float64) (opus.Reader, func(), error) {
			tr.Title = "from p1"
			tr.Duration = time.Minute
			return errFirst{}, func() {}, nil
		}},
		"p2": fakeStreamer{open: func(tr *parsers.Track, _ float64) (opus.Reader, func(), error) {
			tr.Title = "from p2"
			tr.Passthrough = true
			return &pktReader{pkts: [][]byte{{0xAA}}}, func() {}, nil
		}},
	})
	defer SetRegistry(orig)

	track := &parsers.Track{
		Title:      "as resolved",
		SourceInfo: sources.TrackInfo{AvailableParsers: []string{"p1", "p2"}},
	}
	before := *track

	rs := NewRecoveryStream(track)
	if _, err := rs.Open(0); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := rs.ReadPacket(); err != nil { // p1 dies on first read, p2 takes over
		t.Fatalf("ReadPacket: %v", err)
	}

	if !reflect.DeepEqual(*track, before) {
		t.Fatalf("the caller's track was written:\n before %+v\n after  %+v", before, *track)
	}
	if got := rs.Track(); got.Title != "from p2" || !got.Passthrough {
		t.Fatalf("the stream's own copy did not take the parser's writes: %+v", got)
	}
}

// The facts a parser learns are the player's only route back to the Track the
// UI renders, so they have to arrive complete -- both from Open and from the
// confirmation that fires when a switched-to parser first produces audio.
func TestOpenInfoCarriesWhatTheParserLearned(t *testing.T) {
	orig := SetRegistry(map[string]parsers.Streamer{
		"p1": fakeStreamer{open: func(tr *parsers.Track, _ float64) (opus.Reader, func(), error) {
			tr.Title = "opened by p1"
			return errFirst{}, func() {}, nil
		}},
		"p2": fakeStreamer{open: func(tr *parsers.Track, _ float64) (opus.Reader, func(), error) {
			tr.Title = "opened by p2"
			tr.Duration = 3 * time.Minute
			tr.Passthrough = true
			return &pktReader{pkts: [][]byte{{0xAA}}}, func() {}, nil
		}},
	})
	defer SetRegistry(orig)

	track := &parsers.Track{SourceInfo: sources.TrackInfo{AvailableParsers: []string{"p1", "p2"}}}
	rs := NewRecoveryStream(track)

	var confirmed []OpenInfo
	rs.SetOnParserConfirmed(func(info OpenInfo) { confirmed = append(confirmed, info) })

	opened, err := rs.Open(0)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if opened.Parser != "p1" || opened.Title != "opened by p1" {
		t.Fatalf("Open did not report what it opened: %+v", opened)
	}

	if _, err := rs.ReadPacket(); err != nil {
		t.Fatalf("ReadPacket: %v", err)
	}
	if len(confirmed) != 1 {
		t.Fatalf("want one confirmation, got %d", len(confirmed))
	}
	got := confirmed[0]
	if got.Parser != "p2" || got.Title != "opened by p2" || got.Duration != 3*time.Minute || !got.Passthrough {
		t.Fatalf("the confirmation lost what p2 filled in: %+v", got)
	}
}

// Apply is what the player runs under its own lock, so it must not undo a
// resolver's metadata with a parser's silence.
func TestOpenInfoApplyKeepsWhatTheParserDidNotKnow(t *testing.T) {
	track := &parsers.Track{
		Title:         "as resolved",
		Artist:        "resolver",
		Duration:      time.Minute,
		CurrentParser: "p1",
		Passthrough:   true,
	}
	OpenInfo{Parser: "p2"}.Apply(track)

	if track.Title != "as resolved" || track.Artist != "resolver" || track.Duration != time.Minute {
		t.Fatalf("a parser with nothing to add erased the resolver's metadata: %+v", *track)
	}
	if track.CurrentParser != "p2" || track.Passthrough {
		t.Fatalf("what the stream does know must win: %+v", *track)
	}
}
