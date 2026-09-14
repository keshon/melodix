package reply

import (
	"io"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/discord/perm"
)

// Responder answers one interaction, and holds the session and the event that
// answering it needs.
//
// It used to be a singleton taking both as parameters on every call, which is
// what made cmdadapter.Responder a discordgo interface in all but name: the
// two parameters were the library, spelled out twelve times. One of these is
// built per interaction by the handler that received it, so the interface can
// name neither.
type Responder struct {
	s *discordgo.Session
	e *discordgo.InteractionCreate
}

// NewResponder binds a session and an interaction into the reply surface a
// command works with.
func NewResponder(s *discordgo.Session, e *discordgo.InteractionCreate) *Responder {
	return &Responder{s: s, e: e}
}

var _ cmdadapter.Responder = (*Responder)(nil)

func (r *Responder) AckDeferred(ephemeral bool) error {
	if ephemeral {
		return RespondDeferredEphemeral(r.s, r.e)
	}
	return AckDeferred(r.s, r.e)
}

func (r *Responder) RespondEmbed(embed *cmdadapter.Embed, ephemeral bool) error {
	if ephemeral {
		return RespondEmbedEphemeral(r.s, r.e, embed)
	}
	return RespondEmbed(r.s, r.e, embed)
}

func (r *Responder) RespondText(content string, ephemeral bool) error {
	if ephemeral {
		return RespondEphemeral(r.s, r.e, content)
	}
	return Respond(r.s, r.e, content)
}

func (r *Responder) RespondEmbedWithFile(embed *cmdadapter.Embed, src io.Reader, fileName string) error {
	return RespondEmbedEphemeralWithFile(r.s, r.e, embed, src, fileName)
}

func (r *Responder) FollowupEmbed(embed *cmdadapter.Embed, ephemeral bool) error {
	if ephemeral {
		return FollowupEmbedEphemeral(r.s, r.e, embed)
	}
	return FollowupEmbed(r.s, r.e, embed)
}

func (r *Responder) FollowupEmbedWithComponents(embed *cmdadapter.Embed, rows []cmdadapter.ActionRow) error {
	return FollowupEmbedEphemeralWithComponents(r.s, r.e, embed, DiscordComponents(rows))
}

func (r *Responder) FollowupEmbedMessage(embed *cmdadapter.Embed) (string, string, error) {
	return FollowupEmbedMessage(r.s, r.e, embed)
}

func (r *Responder) EditResponseText(content string) error {
	return EditResponse(r.s, r.e, content)
}

func (r *Responder) ReplaceMessage(embed *cmdadapter.Embed) error {
	return ReplaceComponentMessage(r.s, r.e, embed)
}

// API answers what a command asks of the connection rather than of one
// interaction. One of these is good for a whole session.
type API struct {
	s *discordgo.Session
}

// NewSessionAPI wraps a session in the neutral surface.
func NewSessionAPI(s *discordgo.Session) *API {
	return &API{s: s}
}

var _ cmdadapter.SessionAPI = (*API)(nil)

func (a *API) MemberPermissions(userID, channelID string) (int64, error) {
	if a.s == nil {
		return 0, nil
	}
	return a.s.UserChannelPermissions(userID, channelID)
}

func (a *API) CheckBotPermissions(channelID string) bool {
	return perm.CheckBotPermissions(a.s, channelID)
}

func (a *API) CheckBotVoicePermissions(channelID string) (bool, error) {
	return perm.CheckBotVoicePermissions(a.s, channelID)
}

func (a *API) SendChannelMessage(channelID, content string) error {
	return Message(a.s, channelID, content)
}

func (a *API) SendChannelEmbed(channelID string, embed *cmdadapter.Embed) error {
	return MessageEmbed(a.s, channelID, embed)
}

func (a *API) GuildInfo(guildID string) (cmdadapter.GuildInfo, error) {
	return GuildInfo(a.s, guildID)
}

func (a *API) Latency() time.Duration {
	if a.s == nil {
		return 0
	}
	return a.s.HeartbeatLatency()
}

func (a *API) EmbedColor() int { return EmbedColor }
