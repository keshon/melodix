package discordgo

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/disgoorg/godave"
)

func newTestSessionConnection() *VoiceConnection {
	v := newTestVoiceConnection()
	v.session = &Session{State: NewState()}
	v.session.State.User = &User{ID: "1234567890"}
	return v
}

// A caller who configured nothing still gets a session, and it is one that
// holds every frame. The alternative is a nil session, which reads as "this
// channel is not encrypted" and is how plaintext gets out.
func TestNewDAVESessionWithoutAnImplementationHoldsFrames(t *testing.T) {
	v := newTestSessionConnection()

	session := v.newDAVESession()

	if _, ok := session.(unconfiguredSession); !ok {
		t.Fatalf("got %T, want unconfiguredSession", session)
	}
	if !holdFrames(session) {
		t.Fatal("an unconfigured DAVE channel would receive plaintext")
	}
}

// And the point of the hook: the implementation comes from whoever configured
// the Session. This is the seam that lets dave-go in without this package
// knowing anything about MLS — and the same seam disgo's voice stack offers,
// so an implementation configured here carries over unchanged.
func TestNewDAVESessionUsesTheConfiguredImplementation(t *testing.T) {
	v := newTestSessionConnection()

	var (
		gotUserID    godave.UserID
		gotCallbacks godave.Callbacks
		gotLogger    *slog.Logger
	)
	injected := &recordingSession{}
	v.session.DAVESessionCreate = func(logger *slog.Logger, userID godave.UserID, callbacks godave.Callbacks) godave.Session {
		gotLogger, gotUserID, gotCallbacks = logger, userID, callbacks
		return injected
	}

	session := v.newDAVESession()

	if session != godave.Session(injected) {
		t.Fatalf("got %T, want the injected session", session)
	}
	if want := godave.UserID("1234567890"); gotUserID != want {
		t.Fatalf("user id %q, want %q", gotUserID, want)
	}
	if gotLogger == nil {
		t.Fatal("no logger handed to the session; its output would go nowhere")
	}
	// The callbacks have to reach this connection, or the session's commits
	// and key packages are built and then dropped on the floor.
	if callbacks, ok := gotCallbacks.(daveCallbacks); !ok || callbacks.conn != v {
		t.Fatalf("callbacks %#v are not wired to this connection", gotCallbacks)
	}
}

// The session logs per frame when asked to, so the level gate has to be
// answered before a record is built rather than after it is formatted.
func TestDAVELoggerRespectsTheSessionLogLevel(t *testing.T) {
	v := newTestSessionConnection()
	v.LogLevel = LogWarning
	handler := &daveLogHandler{conn: v}

	if handler.Enabled(t.Context(), slog.LevelDebug) {
		t.Error("debug records are being built at LogWarning")
	}
	if handler.Enabled(t.Context(), slog.LevelInfo) {
		t.Error("info records are being built at LogWarning")
	}
	if !handler.Enabled(t.Context(), slog.LevelWarn) {
		t.Error("warnings dropped at LogWarning")
	}
	if !handler.Enabled(t.Context(), slog.LevelError) {
		t.Error("errors dropped at LogWarning")
	}
}

// recordingSession implements godave.Session by embedding it: every method
// this test does not care about is nil and would panic if called, which is the
// point — it keeps the test honest about what the connection actually does.
type recordingSession struct {
	godave.Session
	ssrc  uint32
	codec godave.Codec
	calls int
	added []string
}

func (r *recordingSession) AssignSsrcToCodec(ssrc uint32, codec godave.Codec) {
	r.ssrc, r.codec, r.calls = ssrc, codec, r.calls+1
}

// A session that encrypts per codec refuses to encrypt for an SSRC it was
// never told about, and the refusal arrives once per frame: an error on every
// packet and silence on the channel. The built-in session does not care,
// because it treats everything as Opus, so nothing here caught it until a real
// implementation was plugged in.
func TestAssignDAVECodecAnnouncesOpusForOurSSRC(t *testing.T) {
	v := newTestSessionConnection()
	recorder := &recordingSession{}

	v.assignDAVECodec(recorder, 90986)

	if recorder.calls != 1 {
		t.Fatalf("AssignSsrcToCodec called %d times, want 1 — the session cannot encrypt without it", recorder.calls)
	}
	if recorder.ssrc != 90986 {
		t.Errorf("assigned ssrc %d, want 90986", recorder.ssrc)
	}
	if recorder.codec != godave.CodecOpus {
		t.Errorf("assigned codec %v, want CodecOpus", recorder.codec)
	}
}

// A reconnect re-announces the SSRC without necessarily rebuilding the
// session, and one holding the previous SSRC cannot encrypt for the new one.
func TestAssignDAVECodecFollowsANewSSRC(t *testing.T) {
	v := newTestSessionConnection()
	recorder := &recordingSession{}

	v.assignDAVECodec(recorder, 111)
	v.assignDAVECodec(recorder, 222)

	if recorder.ssrc != 222 {
		t.Fatalf("session still on ssrc %d after the reconnect announced 222", recorder.ssrc)
	}
}

// Before opcode 2 there is no SSRC to assign, and announcing zero would claim
// a stream that does not exist.
func TestAssignDAVECodecWaitsForAnSSRC(t *testing.T) {
	v := newTestSessionConnection()
	recorder := &recordingSession{}

	v.assignDAVECodec(recorder, 0)

	if recorder.calls != 0 {
		t.Fatalf("assigned a codec to ssrc %d before opcode 2 arrived", recorder.ssrc)
	}
}

func (r *recordingSession) AddUser(userID godave.UserID) {
	r.added = append(r.added, string(userID))
}

// Opcode 11 announces the channel roster before opcode 4 says the channel is
// encrypted, so the session that gets built has already missed it. Without the
// replay, dave-go answers an add proposal for that member with "ignoring add
// proposal for unexpected user" and skips the commit — which is the bot
// declining to do the one thing it was given a real MLS implementation for.
func TestAddKnownDAVEMembersReplaysTheRoster(t *testing.T) {
	v := newTestSessionConnection()
	recorder := &recordingSession{}

	v.addKnownDAVEMembers(recorder, []string{"365177820663513089"})

	if len(recorder.added) != 1 || recorder.added[0] != "365177820663513089" {
		t.Fatalf("roster replayed as %v, want the one member announced before the session existed", recorder.added)
	}
}

// lockGrabbingSession stands in for a real DAVE session's lock order. dave-go
// holds its own mutex while it answers the protocol through godave.Callbacks,
// and that sender takes v.Cond.L — so a busy session is holding its mutex and
// waiting for ours. Taking v.Cond.L directly here reproduces the same cycle:
// anything that asks this session a question while holding the lock wedges,
// exactly as the live bot did.
type lockGrabbingSession struct {
	godave.Session
	conn  *VoiceConnection
	ready bool
}

func (s *lockGrabbingSession) Ready() bool {
	s.conn.Cond.L.Lock()
	defer s.conn.Cond.L.Unlock()
	return s.ready
}

// The deadlock that took the bot down: the gate asked the session whether it
// was ready while holding the lock the session needed to answer. It used to
// wait on v.Cond, which can only be done holding v.Cond.L, so there was no way
// to ask the question without holding it.
func TestWaitForDAVEReadyDoesNotHoldTheLockWhileAsking(t *testing.T) {
	v := newTestSessionConnection()
	v.dave = &lockGrabbingSession{conn: v, ready: true}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- v.WaitForDAVEReady(ctx) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("waiting for an already-ready session: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("deadlocked asking the session whether it is ready")
	}
}

// And the gate still gives up when encryption never arrives, rather than
// holding a track forever.
func TestWaitForDAVEReadyStillGivesUp(t *testing.T) {
	v := newTestSessionConnection()
	v.dave = &lockGrabbingSession{conn: v, ready: false}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	if err := v.WaitForDAVEReady(ctx); err == nil {
		t.Fatal("reported ready while the session never was")
	}
}

// A VoiceConnection is kept per guild and reused across channels, so a second
// join arrives with the first channel's answers still in place. Both of these
// are re-announced on every new connection, and keeping the old ones is only
// ever wrong: a session told about absent members would accept an add proposal
// naming them, and a stale SSRC would decrypt somebody's audio under the wrong
// user's ratchet.
func TestResetForNewChannelDropsThePreviousChannelsState(t *testing.T) {
	v := newTestSessionConnection()
	v.daveMembers = map[string]struct{}{"365177820663513089": {}}
	v.ssrcToUserID = map[uint32]string{90986: "365177820663513089"}
	v.dave = &recordingSession{}

	v.resetForNewChannelLocked()

	if len(v.daveMembers) != 0 {
		t.Errorf("roster survived the channel change: %v", v.daveMembers)
	}
	if len(v.ssrcToUserID) != 0 {
		t.Errorf("ssrc map survived the channel change: %v", v.ssrcToUserID)
	}
	// Deliberately kept: opcode 4 replaces it, and nil means "this channel is
	// not encrypted", which is the one answer that lets plaintext out.
	if v.dave == nil {
		t.Error("cleared the session, which reads as an unencrypted channel until opcode 4 arrives")
	}
}
