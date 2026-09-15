package cmdadapter

import (
	"io"
	"time"

	"github.com/rs/zerolog"
)

// The reply surface a command works with.
//
// A command used to open with `s := ctx.Session; e := ctx.Event` and then call
// package-level helpers with both in hand, which is why every command file
// imported discordgo to build the embed it was about to pass. Asking the
// context instead leaves the session and the event where they belong: in the
// layer that knows what they are.
//
// Each interaction context repeats the same methods. They are one line each
// and delegate to the shared helpers below; the alternative was embedding a
// struct, which would have meant rewriting every construction site to say so.
//
// Every helper reports a failed reply before returning it. Commands do not
// check these -- all twenty-seven call sites ignore the error -- and that is
// defensible, because by the time a reply fails there is nothing a command can
// do about it: the interaction it was answering is gone. What is not
// defensible is the silence. A user who saw no answer and a log that says
// nothing happened is the worst pair of facts to debug from, so the report
// happens here, once, rather than at every call site or nowhere.

// reported logs a failed reply and hands the error back unchanged, so a caller
// that does want to act on one still can.
func reported(log zerolog.Logger, kind string, err error) error {
	if err != nil {
		log.Warn().Str("reply", kind).Err(err).Msg("reply_failed")
	}
	return err
}

func ackDeferred(r Responder, log zerolog.Logger, ephemeral bool) error {
	if r == nil {
		return nil
	}
	return reported(log, "defer", r.AckDeferred(ephemeral))
}

func respondEmbed(r Responder, log zerolog.Logger, embed *Embed, ephemeral bool) error {
	if r == nil {
		return nil
	}
	return reported(log, "respond", r.RespondEmbed(embed, ephemeral))
}

func followupEmbed(r Responder, log zerolog.Logger, embed *Embed, ephemeral bool) error {
	if r == nil {
		return nil
	}
	return reported(log, "followup", r.FollowupEmbed(embed, ephemeral))
}

func editResponse(r Responder, log zerolog.Logger, content string) error {
	if r == nil {
		return nil
	}
	return reported(log, "edit", r.EditResponseText(content))
}

func answerEmbedMessage(r Responder, log zerolog.Logger, embed *Embed) (string, string, error) {
	if r == nil {
		return "", "", nil
	}
	channelID, messageID, err := r.AnswerEmbedMessage(embed)
	return channelID, messageID, reported(log, "answer", err)
}

func canJoinVoice(api SessionAPI, channelID string) (bool, error) {
	if api == nil {
		return false, nil
	}
	return api.CheckBotVoicePermissions(channelID)
}

// --- SlashInteractionContext ---

// Defer buys time: Discord wants an acknowledgement within three seconds, and
// resolving a track takes longer than that.
func (c *SlashInteractionContext) Defer() error {
	return ackDeferred(c.Responder, c.AppLog, false)
}

// DeferEphemeral is Defer for a reply only the caller should see.
func (c *SlashInteractionContext) DeferEphemeral() error {
	return ackDeferred(c.Responder, c.AppLog, true)
}

func (c *SlashInteractionContext) Respond(e *Embed) error {
	return respondEmbed(c.Responder, c.AppLog, e, false)
}

func (c *SlashInteractionContext) RespondEphemeral(e *Embed) error {
	return respondEmbed(c.Responder, c.AppLog, e, true)
}

// Followup is what answers a deferred interaction.
func (c *SlashInteractionContext) Followup(e *Embed) error {
	return followupEmbed(c.Responder, c.AppLog, e, false)
}

func (c *SlashInteractionContext) FollowupEphemeral(e *Embed) error {
	return followupEmbed(c.Responder, c.AppLog, e, true)
}

// EditResponseText replaces the original reply with plain text, which is the
// fallback when an embed could not be delivered.
func (c *SlashInteractionContext) EditResponseText(content string) error {
	return editResponse(c.Responder, c.AppLog, content)
}

// RespondEphemeralText answers the caller with plain content. See Responder
// for why this is not the same as an embed carrying the same string.
func (c *SlashInteractionContext) RespondEphemeralText(content string) error {
	if c.Responder == nil {
		return nil
	}
	return c.Responder.RespondText(content, true)
}

// RespondEphemeralWithFile answers the caller with an embed and an attachment
// only they can see. The reader is consumed during the call, so the caller
// keeps ownership of closing it.
func (c *SlashInteractionContext) RespondEphemeralWithFile(embed *Embed, r io.Reader, fileName string) error {
	if c.Responder == nil {
		return nil
	}
	return c.Responder.RespondEmbedWithFile(embed, r, fileName)
}

// FollowupEphemeralWithButtons answers a deferred interaction with controls
// attached. Only the caller sees them, which is what makes a chooser private.
func (c *SlashInteractionContext) FollowupEphemeralWithButtons(embed *Embed, rows ...ActionRow) error {
	if c.Responder == nil {
		return nil
	}
	return c.Responder.FollowupEmbedWithComponents(embed, rows)
}

// Latency is the round trip to Discord's gateway, which is what a ping
// command reports.
func (c *SlashInteractionContext) Latency() time.Duration {
	if c.API == nil {
		return 0
	}
	return c.API.Latency()
}

// Guild describes the guild this command was invoked in.
func (c *SlashInteractionContext) Guild() (GuildInfo, error) {
	if c.API == nil {
		return GuildInfo{}, nil
	}
	return c.API.GuildInfo(c.GuildID())
}

func (c *SlashInteractionContext) CanJoinVoice(channelID string) (bool, error) {
	return canJoinVoice(c.API, channelID)
}

func (c *SlashInteractionContext) AnswerEmbedMessage(embed *Embed) (string, string, error) {
	return answerEmbedMessage(c.Responder, c.AppLog, embed)
}

// --- ComponentInteractionContext ---

func (c *ComponentInteractionContext) Defer() error {
	return ackDeferred(c.Responder, c.AppLog, false)
}

func (c *ComponentInteractionContext) DeferEphemeral() error {
	return ackDeferred(c.Responder, c.AppLog, true)
}

func (c *ComponentInteractionContext) Respond(e *Embed) error {
	return respondEmbed(c.Responder, c.AppLog, e, false)
}

func (c *ComponentInteractionContext) RespondEphemeral(e *Embed) error {
	return respondEmbed(c.Responder, c.AppLog, e, true)
}

func (c *ComponentInteractionContext) Followup(e *Embed) error {
	return followupEmbed(c.Responder, c.AppLog, e, false)
}

func (c *ComponentInteractionContext) FollowupEphemeral(e *Embed) error {
	return followupEmbed(c.Responder, c.AppLog, e, true)
}

func (c *ComponentInteractionContext) EditResponseText(content string) error {
	return editResponse(c.Responder, c.AppLog, content)
}

func (c *ComponentInteractionContext) CanJoinVoice(channelID string) (bool, error) {
	return canJoinVoice(c.API, channelID)
}

func (c *ComponentInteractionContext) AnswerEmbedMessage(embed *Embed) (string, string, error) {
	return answerEmbedMessage(c.Responder, c.AppLog, embed)
}

// ReplaceMessage answers a component interaction by rewriting the message it
// came from, which is how a chooser is consumed: the buttons go away with the
// same click that acts on them, so nothing can be pressed twice.
func (c *ComponentInteractionContext) ReplaceMessage(embed *Embed) error {
	if c.Responder == nil {
		return nil
	}
	return c.Responder.ReplaceMessage(embed)
}

// Interaction is what a shared helper needs from an invocation, whichever kind
// of interaction it arrived as.
//
// Slash commands and component callbacks run the same playback code, and that
// code should not have to know which one it is holding. Every interaction
// context satisfies this.
type Interaction interface {
	GuildID() string
	ChannelID() string
	UserID() string

	Defer() error
	DeferEphemeral() error
	Respond(embed *Embed) error
	RespondEphemeral(embed *Embed) error
	Followup(embed *Embed) error
	FollowupEphemeral(embed *Embed) error
	EditResponseText(content string) error

	// CanJoinVoice reports whether the bot may connect and speak in a channel.
	CanJoinVoice(channelID string) (bool, error)

	// AnswerEmbedMessage makes the embed the interaction's own answer and
	// reports where it landed, so a caller that means to edit it later can
	// find it again. See Responder for why it answers rather than following
	// up.
	AnswerEmbedMessage(embed *Embed) (channelID, messageID string, err error)
}
