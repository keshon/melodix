package discord

import (
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/disgoorg/snowflake/v2"
	"github.com/rs/zerolog"

	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/discord/reply"
	"github.com/keshon/melodix/internal/discord/voice/sink"
	musicsink "github.com/keshon/melodix/pkg/music/sink"
)

// conn is the live connection, whichever library is holding it.
//
// One Bot runs either backend rather than there being two Bots, because
// almost nothing above this is backend-specific: the players, the queues, the
// command guard, the blacklist and the whole VoiceAPI are the same either way.
// What differs is how a reply reaches Discord and how audio gets there, which
// is the two methods below.
//
// It is replaced wholesale when a session opens and cleared when one closes,
// so a caller either gets a live connection or nothing -- never a half-torn
// one.
type conn interface {
	// API is the neutral surface over this connection.
	API() cmdadapter.BotAPI

	// NewSinkProvider builds a guild's audio path on this connection.
	NewSinkProvider(guildID string) musicsink.Provider
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

// discordgoConn is the vendored fork's connection.
//
// It is the one that VOICE_BACKEND applies to: both audio paths can run on
// this gateway, which is what voice-disgo-spike proved, so the choice is a
// real comparison inside one process rather than a restart.
type discordgoConn struct {
	dg         *discordgo.Session
	voiceDelay time.Duration
	log        zerolog.Logger
	// bridge is disgo's voice stack driven by this gateway, or nil when the
	// fork's own is in use. It is shared across guilds, because one voice
	// manager serves all of them.
	bridge *sink.DisgoVoice
}

var _ conn = discordgoConn{}

func (c discordgoConn) API() cmdadapter.BotAPI {
	return reply.NewSessionAPI(c.dg)
}

func (c discordgoConn) NewSinkProvider(guildID string) musicsink.Provider {
	if c.bridge != nil {
		if p, err := c.disgoSinkProvider(guildID); err == nil {
			return p
		} else {
			// Falling back rather than refusing: the bridge needs the bot's
			// own user id, which is only known once READY has landed, and a
			// guild that asked earlier should still get audio.
			c.log.Warn().Str("guild_id", guildID).Err(err).
				Msg("disgo_voice_unavailable_using_discordgo")
		}
	}
	// The provider takes a getter rather than the session because it outlives
	// individual joins; the session it closes over is this connection's, and
	// the provider is discarded with the connection.
	return sink.NewDiscordSinkProvider(
		func() *discordgo.Session { return c.dg },
		guildID, c.voiceDelay, c.log,
	)
}

func (c discordgoConn) disgoSinkProvider(guildID string) (musicsink.Provider, error) {
	gid, err := snowflake.Parse(guildID)
	if err != nil {
		return nil, fmt.Errorf("parsing guild id: %w", err)
	}
	manager, dave, err := c.bridge.Manager(c.dg)
	if err != nil {
		return nil, err
	}
	return sink.NewDisgoSinkProvider(manager, dave, gid, c.voiceDelay, c.log), nil
}

// deadSinkProvider is what a guild gets when it asks for an audio path while
// there is no connection. Returning an error on use rather than nil keeps the
// player's own recovery in charge: it already knows what to do with a sink it
// could not acquire, and a nil provider would panic instead.
type deadSinkProvider struct{}

var _ musicsink.Provider = deadSinkProvider{}

func (deadSinkProvider) Sink(string) (musicsink.AudioSink, error) {
	return nil, errNoConnection
}
func (deadSinkProvider) ReleaseSink(string) {}
func (deadSinkProvider) InvalidateSink()    {}
