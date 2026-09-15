package player

import (
	"os"
	"strings"
	"testing"
)

// The package doc states that p.mu is never held across I/O. This is what
// makes that a fact rather than an intention.
//
// The rule matters because of what the player calls out to: joining a voice
// channel is bounded at fifteen seconds, leaving one at ten, opening a stream
// is a network fetch or an ffmpeg process. Held across any of those, the lock
// guarding a guild's queue is also every reader of that queue's wait -- and
// before commands left the gateway goroutine, that was every guild's wait.
// ReleaseSink was on the wrong side of it for exactly that reason.
//
// The check is deliberately a text scan of one file rather than a type-aware
// analysis. It is looking for three names on three outward edges, all of them
// in this one file, and the honest version of a small rule is a small check
// that says what it cannot see: it does not follow calls, so a helper invoked
// under the lock that itself does I/O passes this and is still wrong.
func TestLockIsNeverHeldAcrossIO(t *testing.T) {
	// The player's edges to the outside world, named by method rather than by
	// receiver. Receiver names do not survive the fix: the way to get a call
	// out from under a lock is to capture what it is called on into a local
	// first, so a check keyed on "p.sinkProvider." goes quiet at precisely the
	// moment somebody puts "provider." back inside the lock.
	outward := []string{
		".Sink(", ".ReleaseSink(", ".InvalidateSink(", // sink.Provider
		".Stream(",                                // sink.AudioSink
		".Resolve(",                               // player.Resolver
		".Start(", ".RequestReopen(", ".Packets(", // stream.RecoveryStream
		".ReadPacket(",
	}

	src, err := os.ReadFile("player.go")
	if err != nil {
		t.Fatalf("reading player.go: %v", err)
	}

	depth := 0
	openedAt := 0
	for i, line := range strings.Split(string(src), "\n") {
		lineNo := i + 1
		code := line
		if c := strings.Index(code, "//"); c >= 0 {
			code = code[:c]
		}

		if strings.Contains(code, "p.mu.Lock()") {
			depth++
			if depth == 1 {
				openedAt = lineNo
			}
			continue
		}
		if strings.Contains(code, "p.mu.Unlock()") {
			if depth > 0 {
				depth--
			}
			continue
		}
		if depth == 0 {
			continue
		}
		for _, edge := range outward {
			if strings.Contains(code, edge) {
				t.Errorf("player.go:%d calls %s while holding p.mu (taken at line %d):\n\t%s",
					lineNo, strings.TrimSuffix(edge, "."), openedAt, strings.TrimSpace(line))
			}
		}
	}

	if depth != 0 {
		t.Fatalf("scan ended inside %d unclosed p.mu region(s); the check cannot be trusted", depth)
	}
}

// A Track that leaves the engine leaves by value. A caller holding a pointer
// into the player is a caller who can write what a playback run is reading,
// which was a live race on the default configuration and is the reason this
// package's public shape changed.
//
// Checked against the signatures rather than against behaviour, because that
// is where it is decided: an exported method cannot hand out a pointer it does
// not have in its signature, and adding one back is a visible edit here.
func TestNoExportedMethodHandsOutATrackPointer(t *testing.T) {
	src, err := os.ReadFile("player.go")
	if err != nil {
		t.Fatalf("reading player.go: %v", err)
	}

	for i, line := range strings.Split(string(src), "\n") {
		if !strings.HasPrefix(line, "func (p *Player) ") {
			continue
		}
		name := strings.TrimPrefix(line, "func (p *Player) ")
		if name == "" || !strings.ContainsAny(name[:1], "ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
			continue // unexported: the engine's own business
		}
		if strings.Contains(line, "*parsers.Track") {
			t.Errorf("player.go:%d exports a *parsers.Track across the engine boundary:\n\t%s",
				i+1, strings.TrimSpace(line))
		}
	}
}
