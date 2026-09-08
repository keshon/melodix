package discord

import (
	"io"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog"
)

// wedgedSession returns a session whose mutex is held and never released,
// which is what a gateway read with no deadline does to every other reader of
// the session. Nothing unlocks it: that is the condition under test.
func wedgedSession(t *testing.T) *discordgo.Session {
	t.Helper()
	dg, err := discordgo.New("Bot test-token")
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	dg.Lock()
	return dg
}

func TestLastHeartbeatAckReadsALiveSession(t *testing.T) {
	dg, err := discordgo.New("Bot test-token")
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	want := time.Unix(1_700_000_000, 0).UTC()
	dg.LastHeartbeatAck = want

	got, ok := lastHeartbeatAck(dg, time.Second)
	if !ok {
		t.Fatal("gave up on a session whose mutex was free")
	}
	if !got.Equal(want) {
		t.Fatalf("ack = %s, want %s", got, want)
	}
}

func TestLastHeartbeatAckGivesUpOnWedgedSession(t *testing.T) {
	dg := wedgedSession(t)

	start := time.Now()
	if _, ok := lastHeartbeatAck(dg, 50*time.Millisecond); ok {
		t.Fatal("read succeeded against a mutex nobody releases")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("gave up after %s — the timeout is not bounding the read", elapsed)
	}
}

func TestCloseSessionAbandonsWedgedSession(t *testing.T) {
	dg := wedgedSession(t)
	log := zerolog.New(io.Discard)

	done := make(chan struct{})
	go func() {
		defer close(done)
		closeSession(dg, 50*time.Millisecond, log)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		// A blocking close here is RunSession's last statement, so it strands
		// the restart loop in main and the bot never reconnects.
		t.Fatal("closeSession blocked on a wedged session")
	}
}
