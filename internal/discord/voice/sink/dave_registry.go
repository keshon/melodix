package sink

import (
	"log/slog"
	"sync"
	"time"

	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/godave"
	"github.com/disgoorg/snowflake/v2"
	davesession "github.com/thomas-vilte/dave-go/session"
)

// DaveRegistry keeps each guild connection's DAVE session, which is the only
// way to ask whether end-to-end encryption has come up.
//
// disgo v0.19.6 exposes no route from a Conn to its session -- Conn.DAVE()
// exists only on the unreleased branch -- so it has to be caught as the
// session is built. The spike did that with a pending field written by the
// session hook and read back after CreateConn, which meant every CreateConn
// had to go through one type while holding one mutex, and a connection
// created any other way silently had no session recorded.
//
// This does it with WithConnCreateFunc instead. The guild id is a parameter of
// the create func, so the hook can close over the guild it is being built for
// and file the session in the right slot by construction. CreateConn may then
// be called from anywhere, including by disgo's own machinery.
//
// The create func runs while the Manager holds its own connsMu, so the map
// below needs its own lock. That is safe here because this only stores the
// pointer: it never calls into the session, which is what the deadlock rule
// forbids -- a godave.Session holds its own mutex while answering the
// protocol, so anything that asks it a question under a connection lock
// deadlocks both.
type DaveRegistry struct {
	mu       sync.Mutex
	sessions map[snowflake.ID]*davesession.Session
}

// NewDaveRegistry creates an empty registry.
func NewDaveRegistry() *DaveRegistry {
	return &DaveRegistry{sessions: make(map[snowflake.ID]*davesession.Session)}
}

// Session is the guild's DAVE session, or nil if none was built -- which
// would mean disgo changed when it creates one.
func (r *DaveRegistry) Session(guildID snowflake.ID) *davesession.Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sessions[guildID]
}

// Forget drops and closes the guild's session. dave-go arms recovery
// watchdogs that keep re-arming invalidations on a channel the bot has left
// until they expire, so closing matters even though it is bounded.
func (r *DaveRegistry) Forget(guildID snowflake.ID) error {
	r.mu.Lock()
	s := r.sessions[guildID]
	delete(r.sessions, guildID)
	r.mu.Unlock()

	if s == nil {
		return nil
	}
	return s.Close()
}

func (r *DaveRegistry) put(guildID snowflake.ID, s *davesession.Session) {
	r.mu.Lock()
	r.sessions[guildID] = s
	r.mu.Unlock()
}

// ManagerOptions are the voice.Manager options that route every connection's
// DAVE session into this registry.
//
// dave-go is a full RFC 9420 implementation in pure Go, and "full" is the
// operative word -- it can commit to an MLS group, so the bot holds its own
// epoch while alone in a channel. Do not swap it for disgoorg/godave's own
// session: that one links libdave over cgo, and release.yml cross-compiles
// six targets with CGO_ENABLED=0 from Linux runners, which a C++ dependency
// ends.
func (r *DaveRegistry) ManagerOptions(logger *slog.Logger) []voice.ManagerConfigOpt {
	return []voice.ManagerConfigOpt{
		voice.WithLogger(logger),
		voice.WithDaveSessionLogger(logger),
		voice.WithConnCreateFunc(func(
			guildID snowflake.ID,
			userID snowflake.ID,
			stateUpdate voice.StateUpdateFunc,
			removeConn func(),
			opts ...voice.ConnConfigOpt,
		) voice.Conn {
			create := davesession.CreateFunc(
				davesession.WithRecoveryTimeout(DaveRecoveryTimeout),
				davesession.WithSessionHook(func(s *davesession.Session) {
					r.put(guildID, s)
				}),
			)
			opts = append(opts, voice.WithConnDaveSessionCreateFunc(
				godave.SessionCreateFunc(create),
			))
			return voice.NewConn(guildID, userID, stateUpdate, removeConn, opts...)
		}),
	}
}

// DaveRecoveryTimeout bounds how long a committed epoch may sit unactivated
// before dave-go declares the MLS state broken and re-requests a key package.
//
// Its default is 15 seconds, which is chosen for a call: a caller who cannot
// be heard for fifteen seconds asks whether anyone can hear them. Nobody asks
// a music bot anything -- they hear silence and assume it is broken, and every
// frame sent meanwhile is encrypted under an epoch the listeners left. Watched
// happening: after a listener left and rejoined, the bot committed them back
// in, the gateway never announced the transition, and 796 frames went out on a
// dead epoch over twelve seconds.
//
// Five seconds is a guess, not a measurement. It is meant to leave room for a
// slow or lossy link to complete a round trip that normally takes about a
// tenth of a second, without the round trip being retried into a re-key storm.
// If re-keys start appearing on a link that used to be quiet, this is the
// number that bought them.
const DaveRecoveryTimeout = 5 * time.Second
