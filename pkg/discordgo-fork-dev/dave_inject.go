package discordgo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/disgoorg/godave"
)

// newDAVESession builds the session for a connection that has just been told
// the channel is end-to-end encrypted.
//
// Which implementation that is belongs to whoever configured the Session, not
// to this package. Nothing here knows how to commit to an MLS group, and
// nothing here should have to: the connection's job is the websocket and the
// opcodes, and the cryptography behind godave.Session is a separate decision
// with separate trade-offs — pure Go or cgo, one library or another, or none
// at all on a channel that never negotiated encryption.
//
// The default is the package's own session, so a caller that sets nothing gets
// what this fork has always done.
// daveResetter is implemented by DAVE sessions that can be returned to their
// initial state without being replaced. godave.Session does not require it,
// because the implementations that inspired the interface are rebuilt on
// reconnect instead; this connection resumes without rebuilding, so it asks.
type daveResetter interface {
	Reset()
}

// holdFrames reports whether the sender must not transmit right now.
//
// A nil session means the channel never negotiated encryption — v.dave is only
// built when op 4 reports a DAVE version above zero — and plaintext is what
// belongs on the wire there. A session that exists but is not ready is the
// dangerous case: the channel is end-to-end encrypted and there is no epoch,
// so anything sent goes out in the clear, in a channel that promised
// otherwise, to receivers that will discard it and a server entitled to close
// us for it.
//
// This is godave's own contract, not an invention here: Ready is documented as
// the question an audio sender asks before every frame, precisely so it holds
// rather than sending unencrypted.
func holdFrames(dave godave.Session) bool {
	return dave != nil && !dave.Ready()
}

// errNoDAVEImplementation is what an unconfigured session answers with. It is
// never nil-checked for identity; it exists so the failure has words.
var errNoDAVEImplementation = errors.New("dave: no session implementation configured")

// unconfiguredSession stands in when the channel is end-to-end encrypted and
// nobody told this Session how to do that.
//
// The alternative would be to leave v.dave nil, and nil means "this channel is
// not encrypted" — the one answer that puts plaintext on an encrypted channel.
// This is never ready, so the sender holds every frame, and it refuses to
// encrypt or decrypt so nothing can slip past by another route. Connected and
// silent, which is the safe direction, and the log says why once per session.
type unconfiguredSession struct {
	godave.Session
}

func (unconfiguredSession) MaxSupportedProtocolVersion() int             { return daveProtocolVersion }
func (unconfiguredSession) Ready() bool                                  { return false }
func (unconfiguredSession) SetChannelID(godave.ChannelID)                {}
func (unconfiguredSession) AssignSsrcToCodec(uint32, godave.Codec)       {}
func (unconfiguredSession) AddUser(godave.UserID)                        {}
func (unconfiguredSession) RemoveUser(godave.UserID)                     {}
func (unconfiguredSession) MaxEncryptedFrameSize(frameSize int) int      { return frameSize }
func (unconfiguredSession) MaxDecryptedFrameSize(godave.UserID, int) int { return 0 }

func (unconfiguredSession) Encrypt(uint32, []byte, []byte) (int, error) {
	return 0, errNoDAVEImplementation
}

func (unconfiguredSession) Decrypt(godave.UserID, []byte, []byte) (int, error) {
	return 0, errNoDAVEImplementation
}

func (unconfiguredSession) OnSelectProtocolAck(uint16)                      {}
func (unconfiguredSession) OnDavePrepareTransition(uint16, uint16)          {}
func (unconfiguredSession) OnDaveExecuteTransition(uint16)                  {}
func (unconfiguredSession) OnDavePrepareEpoch(int, uint16)                  {}
func (unconfiguredSession) OnDaveMLSExternalSenderPackage([]byte)           {}
func (unconfiguredSession) OnDaveMLSProposals([]byte)                       {}
func (unconfiguredSession) OnDaveMLSPrepareCommitTransition(uint16, []byte) {}
func (unconfiguredSession) OnDaveMLSWelcome(uint16, []byte)                 {}

func (v *VoiceConnection) newDAVESession() godave.Session {
	userID := v.session.State.User.ID

	create := v.session.DAVESessionCreate
	if create == nil {
		v.log(LogError, "DAVE channel but Session.DAVESessionCreate is nil; holding all audio")
		return unconfiguredSession{}
	}

	v.log(LogInformational, "DAVE building the configured session implementation")
	return create(v.daveLogger(), godave.UserID(userID), daveCallbacks{conn: v})
}

// Lock order, and the reason these two take their arguments rather than
// reading them off the connection.
//
// A DAVE session holds its own mutex while it works, and while holding it, it
// answers the protocol through godave.Callbacks -- which is this connection's
// sender, which takes v.Cond.L. So a session that is busy is holding its
// mutex and waiting for v.Cond.L.
//
// Anything on this side that holds v.Cond.L and then asks the session a
// question -- Ready(), AddUser, AssignSsrcToCodec, all of which take the
// session's mutex -- closes the cycle, and both goroutines stop. Watched
// happening: the opus sender asked Ready() under the lock at the moment
// proposals arrived, dave-go was mid-commit holding its mutex and blocked
// sending the commit welcome, and the bot went silent and would not shut down.
//
// So: **never call into a godave.Session while holding v.Cond.L.** Read what
// you need from the connection under the lock, release it, then talk to the
// session. These two take a session and a value for that reason; passing the
// connection's fields would invite the caller to hold the lock while doing it.

// assignDAVECodec tells the session which codec our own SSRC carries.
//
// Not optional and not bookkeeping: a session that keys frame encryption by
// codec refuses an SSRC it has never been told about, which arrives as an
// error on every frame and silence on the channel. The built-in session
// ignores it because it treats everything as Opus, which is exactly why the
// call was easy to leave out.
//
// Opus is the only codec this connection sends. The SSRC comes from opcode 2,
// which arrives before opcode 4 builds the session, and is re-announced on
// reconnect -- which is why opcode 2 makes this call too.
func (v *VoiceConnection) assignDAVECodec(session godave.Session, ssrc uint32) {
	if session == nil || ssrc == 0 {
		return
	}
	v.log(LogDebug, "DAVE assigning codec opus to ssrc %d", ssrc)
	session.AssignSsrcToCodec(ssrc, godave.CodecOpus)
}

// addKnownDAVEMembers replays the channel roster into a new session.
//
// Opcode 11 announces who is in the channel before opcode 4 says the channel
// is encrypted, so a session built at opcode 4 has missed it. A session that
// has not been told about a member will not commit an add proposal naming
// them -- it logs "ignoring add proposal for unexpected user" and skips the
// commit, which leaves the group waiting on somebody else to do the work this
// implementation was chosen to be able to do.
func (v *VoiceConnection) addKnownDAVEMembers(session godave.Session, userIDs []string) {
	if session == nil {
		return
	}
	for _, userID := range userIDs {
		v.log(LogDebug, "DAVE adding known member %s", userID)
		session.AddUser(godave.UserID(userID))
	}
}

// resetForNewChannelLocked drops the state that belongs to the channel this
// connection was last in.
//
// VoiceConnections are kept per guild and reused, so joining a second channel
// arrives here with the first one's answers still in place. Both of these are
// re-announced on every new connection -- opcode 11 names who is present,
// opcode 5 names who owns which SSRC -- so keeping the old ones gains nothing
// and costs two ways: a DAVE session told about members who are not here would
// accept an add proposal naming them, and a stale SSRC would decrypt somebody
// else's audio with the wrong user's ratchet.
//
// v.dave is deliberately left alone. Opcode 4 replaces it under this same lock
// before the sender starts, and a nil session means "this channel is not
// encrypted", which is the one answer that lets plaintext out -- not a state
// worth passing through on the way to the right one.
//
// Callers must hold v.Cond.L.
func (v *VoiceConnection) resetForNewChannelLocked() {
	v.daveMembers = nil
	v.ssrcToUserID = nil
}

// daveLogger hands the session a logger that comes out where every other line
// from this connection does, gated by the same LogLevel. A DAVE session logs
// per frame when asked to, so the gate has to be answered before the record is
// built rather than after.
func (v *VoiceConnection) daveLogger() *slog.Logger {
	return slog.New(&daveLogHandler{conn: v})
}

// daveLogHandler is the smallest slog.Handler that can carry a session's
// output into discordgo's logging. It formats attributes as key=value because
// that is what the rest of these lines look like; anything richer belongs in
// the caller's own logger, which is free to replace this one by handing the
// session a different implementation.
type daveLogHandler struct {
	conn   *VoiceConnection
	attrs  []slog.Attr
	groups []string
}

func (h *daveLogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return daveLogLevel(level) <= h.conn.LogLevel
}

func (h *daveLogHandler) Handle(_ context.Context, record slog.Record) error {
	var line strings.Builder
	line.WriteString(record.Message)

	prefix := ""
	if len(h.groups) > 0 {
		prefix = strings.Join(h.groups, ".") + "."
	}
	write := func(attr slog.Attr) bool {
		fmt.Fprintf(&line, " %s%s=%v", prefix, attr.Key, attr.Value.Any())
		return true
	}
	for _, attr := range h.attrs {
		write(attr)
	}
	record.Attrs(write)

	h.conn.log(daveLogLevel(record.Level), "DAVE session: %s", line.String())
	return nil
}

func (h *daveLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	next.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &next
}

func (h *daveLogHandler) WithGroup(name string) slog.Handler {
	next := *h
	next.groups = append(append([]string(nil), h.groups...), name)
	return &next
}

// daveLogLevel maps slog's levels onto discordgo's, which run the other way:
// LogError is 0 and LogDebug is 3, and a message is dropped when its level is
// greater than the session's.
func daveLogLevel(level slog.Level) int {
	switch {
	case level >= slog.LevelError:
		return LogError
	case level >= slog.LevelWarn:
		return LogWarning
	case level >= slog.LevelInfo:
		return LogInformational
	default:
		return LogDebug
	}
}
