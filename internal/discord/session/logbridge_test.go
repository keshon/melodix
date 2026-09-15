package session

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

// frameRecord is dave-go's per-frame record, with the attributes it actually
// carries (session.go, encryptLocked).
func frameRecord(retained bool) slog.Record {
	r := slog.Record{Level: slog.LevelDebug, Message: frameEncrypted}
	r.Add("ssrc", 119505, "epoch", 1, "retained", retained, "generation", 0)
	return r
}

func decodeEvents(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var e map[string]any
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("log line is not JSON: %v (%q)", err, line)
		}
		out = append(out, e)
	}
	return out
}

func testBridge(buf *bytes.Buffer, level zerolog.Level) *bridge {
	return &bridge{log: zerolog.New(buf).Level(level), frames: &frameCounter{}}
}

// The defect this was written for: severity lived in the text the old bridge
// was searching, so every record came out at info. A Discord outage -- pages
// of level=ERROR from the gateway -- read as idle chatter, and did so in front
// of someone reading the log to find out what was wrong.
func TestALibraryErrorIsLoggedAsAnError(t *testing.T) {
	var buf bytes.Buffer
	b := testBridge(&buf, zerolog.InfoLevel)

	r := slog.Record{Level: slog.LevelError, Message: "failed to reconnect gateway"}
	r.Add("err", "context deadline exceeded", "try", 0)
	if err := b.Handle(context.Background(), r); err != nil {
		t.Fatalf("handle: %v", err)
	}

	events := decodeEvents(t, &buf)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0]["level"] != "error" {
		t.Fatalf("a gateway ERROR was logged as %q", events[0]["level"])
	}
	if events[0]["msg"] != "failed to reconnect gateway" {
		t.Fatalf("the library's own message did not survive: %v", events[0])
	}
	if events[0]["err"] != "context deadline exceeded" {
		t.Fatalf("attributes are fields now, not prose: %v", events[0])
	}
}

// One record per 20ms of audio is fifty a second. At LOG_LEVEL=debug that
// buried every other line in the file and kept the console busy for the length
// of every track.
func TestPerFrameRecordsAreCountedNotPrinted(t *testing.T) {
	var buf bytes.Buffer
	b := testBridge(&buf, zerolog.DebugLevel)

	for i := 0; i < 5000; i++ {
		if err := b.Handle(context.Background(), frameRecord(false)); err != nil {
			t.Fatalf("handle: %v", err)
		}
	}

	if events := decodeEvents(t, &buf); len(events) != 0 {
		t.Fatalf("%d lines emitted for 5000 frames; want none before the window elapses", len(events))
	}
}

// The counts are the point of keeping them at all: a frame that went out under
// the previous epoch is one a member who just joined cannot decrypt.
func TestTheTallyReportsWhatTheFramesSaid(t *testing.T) {
	var buf bytes.Buffer
	b := testBridge(&buf, zerolog.DebugLevel)

	for i := 0; i < 100; i++ {
		_ = b.Handle(context.Background(), frameRecord(false))
	}
	for i := 0; i < 40; i++ {
		_ = b.Handle(context.Background(), frameRecord(true))
	}

	b.frames.mu.Lock()
	total, retained := b.frames.total, b.frames.retained
	b.frames.mu.Unlock()
	if total != 140 || retained != 40 {
		t.Fatalf("counted %d frames, %d retained; want 140 and 40", total, retained)
	}
}

// Everything that is not a frame still reaches the log, or suppressing the
// flood would take the diagnostics with it -- and a send failure keeps the
// name that makes it worth alerting on.
func TestEveryOtherLibraryRecordStillGetsThrough(t *testing.T) {
	var buf bytes.Buffer
	b := testBridge(&buf, zerolog.DebugLevel)

	_ = b.Handle(context.Background(), frameRecord(false))
	_ = b.Handle(context.Background(), slog.Record{Level: slog.LevelDebug, Message: "epoch activated"})
	_ = b.Handle(context.Background(), slog.Record{Level: slog.LevelError, Message: audioSendFailure})

	events := decodeEvents(t, &buf)
	if len(events) != 2 {
		t.Fatalf("got %d events, want the two non-frame records", len(events))
	}
	if events[0]["message"] != "library_log" || events[0]["msg"] != "epoch activated" {
		t.Fatalf("the epoch record did not survive: %v", events[0])
	}
	if events[1]["message"] != "voice_audio_send_failed" {
		t.Fatalf("a send failure lost its name: %v", events[1])
	}
}

// LOG_LEVEL has to reach the libraries, or dave-go's debug records arrive at
// fifty a second to be discarded one at a time -- and, worse, their absence
// gets read as the frames not happening. That misreading has already cost one
// wrong diagnosis here.
func TestTheAppsLevelGatesTheLibraries(t *testing.T) {
	var buf bytes.Buffer
	b := testBridge(&buf, zerolog.InfoLevel)

	if b.Enabled(context.Background(), slog.LevelDebug) {
		t.Fatal("debug records reach a bridge configured at info")
	}
	if !b.Enabled(context.Background(), slog.LevelError) {
		t.Fatal("error records do not reach a bridge configured at info")
	}
}
