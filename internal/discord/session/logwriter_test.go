package session

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

// Taken verbatim from a live run, because the only thing holding the parsing
// together is dave-go's line format and a made-up example would not notice it
// changing.
const liveFrameLine = `time=2026-09-15T13:50:41.883+03:00 level=DEBUG ` +
	`msg="frame encrypted" dave_session=a8e3b1aa user_id=1487751369854030024 ` +
	`channel_id=1487736396708970560 ssrc=119505 epoch=1 retained=false nonce=1 ` +
	`generation=0 plaintext_size=324 encrypted_size=336`

// The same event as this project's logs carried it before the library's
// formatting changed. Both have to be recognised, or a library upgrade
// silently restores the flood.
const olderFrameLine = `DAVE session: frame encrypted dave_session=2234a643 ` +
	`user_id=1487751369854030024 ssrc=41675 epoch=1 retained=false nonce=1 ` +
	`generation=0 plaintext_size=324 encrypted_size=336`

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

// One line per 20ms of audio is fifty a second. At LOG_LEVEL=debug that buried
// every other line in the file and kept the console busy for the length of
// every track.
func TestPerFrameLinesAreCountedNotPrinted(t *testing.T) {
	var buf bytes.Buffer
	w := logWriter{log: zerolog.New(&buf), frames: &frameCounter{}}

	for i := 0; i < 5000; i++ {
		if _, err := w.Write([]byte(liveFrameLine + "\n")); err != nil {
			t.Fatalf("write: %v", err)
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
	counter := &frameCounter{}
	w := logWriter{log: zerolog.New(&buf), frames: counter}

	retained := strings.Replace(liveFrameLine, "retained=false", "retained=true", 1)
	for i := 0; i < 100; i++ {
		_, _ = w.Write([]byte(liveFrameLine + "\n"))
	}
	for i := 0; i < 40; i++ {
		_, _ = w.Write([]byte(retained + "\n"))
	}

	counter.mu.Lock()
	total, ret := counter.total, counter.retained
	counter.mu.Unlock()
	if total != 140 || ret != 40 {
		t.Fatalf("counted %d frames, %d retained; want 140 and 40", total, ret)
	}
}

// Everything that is not a frame still reaches the log, or suppressing the
// flood would take the diagnostics with it.
func TestEveryOtherLibraryLineStillGetsThrough(t *testing.T) {
	var buf bytes.Buffer
	w := logWriter{log: zerolog.New(&buf), frames: &frameCounter{}}

	_, _ = w.Write([]byte(liveFrameLine + "\n"))
	_, _ = w.Write([]byte(`level=DEBUG msg="epoch activated" epoch_id=3 sender_count=2` + "\n"))
	_, _ = w.Write([]byte(`level=ERROR msg="failed to send audio" err=broken` + "\n"))

	events := decodeEvents(t, &buf)
	if len(events) != 2 {
		t.Fatalf("got %d events, want the two non-frame lines", len(events))
	}
	if events[0]["message"] != "disgo_log" || !strings.Contains(events[0]["raw"].(string), "epoch activated") {
		t.Fatalf("the epoch line did not survive: %v", events[0])
	}
	if events[1]["message"] != "voice_audio_send_failed" {
		t.Fatalf("a send failure lost its name: %v", events[1])
	}
}
