package cmdadapter

import "github.com/bwmarrin/discordgo"

// The reply surface a command works with.
//
// A command used to open with `s := ctx.Session; e := ctx.Event` and then call
// package-level helpers with both in hand, which is why every command file
// imported discordgo to build the embed it was about to pass. Asking the
// context instead leaves the session and the event where they belong: in the
// layer that knows what they are.
//
// Each interaction context repeats the same six methods. They are one line
// each and delegate to the shared helpers below; the alternative was embedding
// a struct, which would have meant rewriting every construction site to say so.

func ackDeferred(r Responder, s *discordgo.Session, e *discordgo.InteractionCreate, ephemeral bool) error {
	if r == nil {
		return nil
	}
	if ephemeral {
		return r.AckDeferredEphemeral(s, e)
	}
	return r.AckDeferred(s, e)
}

func respondEmbed(r Responder, s *discordgo.Session, e *discordgo.InteractionCreate, embed *Embed, ephemeral bool) error {
	if r == nil {
		return nil
	}
	if ephemeral {
		return r.RespondEmbedEphemeral(s, e, discordEmbed(embed))
	}
	return r.RespondEmbed(s, e, discordEmbed(embed))
}

func followupEmbed(r Responder, s *discordgo.Session, e *discordgo.InteractionCreate, embed *Embed, ephemeral bool) error {
	if r == nil {
		return nil
	}
	if ephemeral {
		return r.FollowupEmbedEphemeral(s, e, discordEmbed(embed))
	}
	return r.FollowupEmbed(s, e, discordEmbed(embed))
}

func editResponse(r Responder, s *discordgo.Session, e *discordgo.InteractionCreate, content string) error {
	if r == nil {
		return nil
	}
	return r.EditResponse(s, e, content)
}

// --- SlashInteractionContext ---

// Defer buys time: Discord wants an acknowledgement within three seconds, and
// resolving a track takes longer than that.
func (c *SlashInteractionContext) Defer() error {
	return ackDeferred(c.Responder, c.Session, c.Event, false)
}

// DeferEphemeral is Defer for a reply only the caller should see.
func (c *SlashInteractionContext) DeferEphemeral() error {
	return ackDeferred(c.Responder, c.Session, c.Event, true)
}

func (c *SlashInteractionContext) Respond(e *Embed) error {
	return respondEmbed(c.Responder, c.Session, c.Event, e, false)
}

func (c *SlashInteractionContext) RespondEphemeral(e *Embed) error {
	return respondEmbed(c.Responder, c.Session, c.Event, e, true)
}

// Followup is what answers a deferred interaction.
func (c *SlashInteractionContext) Followup(e *Embed) error {
	return followupEmbed(c.Responder, c.Session, c.Event, e, false)
}

func (c *SlashInteractionContext) FollowupEphemeral(e *Embed) error {
	return followupEmbed(c.Responder, c.Session, c.Event, e, true)
}

// EditResponseText replaces the original reply with plain text, which is the
// fallback when an embed could not be delivered.
func (c *SlashInteractionContext) EditResponseText(content string) error {
	return editResponse(c.Responder, c.Session, c.Event, content)
}

// --- ComponentInteractionContext ---

func (c *ComponentInteractionContext) Defer() error {
	return ackDeferred(c.Responder, c.Session, c.Event, false)
}

func (c *ComponentInteractionContext) DeferEphemeral() error {
	return ackDeferred(c.Responder, c.Session, c.Event, true)
}

func (c *ComponentInteractionContext) Respond(e *Embed) error {
	return respondEmbed(c.Responder, c.Session, c.Event, e, false)
}

func (c *ComponentInteractionContext) RespondEphemeral(e *Embed) error {
	return respondEmbed(c.Responder, c.Session, c.Event, e, true)
}

func (c *ComponentInteractionContext) Followup(e *Embed) error {
	return followupEmbed(c.Responder, c.Session, c.Event, e, false)
}

func (c *ComponentInteractionContext) FollowupEphemeral(e *Embed) error {
	return followupEmbed(c.Responder, c.Session, c.Event, e, true)
}

func (c *ComponentInteractionContext) EditResponseText(content string) error {
	return editResponse(c.Responder, c.Session, c.Event, content)
}

// --- MessageApplicationCommandContext ---

func (c *MessageApplicationCommandContext) Defer() error {
	return ackDeferred(c.Responder, c.Session, c.Event, false)
}

func (c *MessageApplicationCommandContext) DeferEphemeral() error {
	return ackDeferred(c.Responder, c.Session, c.Event, true)
}

func (c *MessageApplicationCommandContext) Respond(e *Embed) error {
	return respondEmbed(c.Responder, c.Session, c.Event, e, false)
}

func (c *MessageApplicationCommandContext) RespondEphemeral(e *Embed) error {
	return respondEmbed(c.Responder, c.Session, c.Event, e, true)
}

func (c *MessageApplicationCommandContext) Followup(e *Embed) error {
	return followupEmbed(c.Responder, c.Session, c.Event, e, false)
}

func (c *MessageApplicationCommandContext) FollowupEphemeral(e *Embed) error {
	return followupEmbed(c.Responder, c.Session, c.Event, e, true)
}

func (c *MessageApplicationCommandContext) EditResponseText(content string) error {
	return editResponse(c.Responder, c.Session, c.Event, content)
}
