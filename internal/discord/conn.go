package discord

import (
	disgovoice "github.com/disgoorg/disgo/voice"

	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/discord/voice/sink"
	musicsink "github.com/keshon/melodix/pkg/music/sink"
)

// conn is the live connection.
//
// It is replaced wholesale when a session opens and cleared when one closes,
// so a caller either gets a live connection or nothing -- never a half-torn
// one. Everything above it -- the players, the queues, the command guard, the
// blacklist, the whole VoiceAPI -- outlives any single session and reaches
// Discord only through these two methods.
type conn interface {
	// API is the neutral surface over this connection.
	API() cmdadapter.BotAPI

	// VoiceResources are the parts of this session a voice connection is built
	// from. They are handed out rather than built into anything, because both
	// die with the session and the things that need them do not.
	VoiceResources() (disgovoice.Manager, *sink.DaveRegistry)
}

// connHolder boxes the interface so it can live in an atomic.Value, which
// needs a single concrete type.
type connHolder struct {
	c conn
}

// setConn publishes a live connection; clearConn withdraws it.
func (b *Bot) setConn(c conn) { b.conn.Store(&connHolder{c: c}) }
func (b *Bot) clearConn()     { b.conn.Store(&connHolder{}) }

// currentConn is the live connection, or nil between sessions.
func (b *Bot) currentConn() conn {
	v := b.conn.Load()
	if v == nil {
		return nil
	}
	holder, ok := v.(*connHolder)
	if !ok || holder == nil {
		return nil
	}
	return holder.c
}

// deadSinkProvider is what a guild gets when its id is not a snowflake, which
// means nothing about this guild will ever work. Returning an error on use
// rather than nil keeps the player's own recovery in charge: it already knows
// what to do with a sink it could not acquire, and a nil provider would panic
// instead.
//
// Having no session is not this: a real provider answers that itself, and
// starts working again when a session comes back.
type deadSinkProvider struct{}

var _ musicsink.Provider = deadSinkProvider{}

func (deadSinkProvider) Sink(string) (musicsink.AudioSink, error) {
	return nil, errNoConnection
}
func (deadSinkProvider) ReleaseSink(string) {}
func (deadSinkProvider) InvalidateSink()    {}
