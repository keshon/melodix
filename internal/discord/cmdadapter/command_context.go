package cmdadapter

import (
	"github.com/keshon/command"
	"github.com/keshon/melodix/internal/storage"
)

// CommandContext is everything a middleware needs to know about an invocation
// without naming the library the invocation came from.
//
// There are five context types and a middleware cares about the difference
// between roughly none of them: it wants the guild, the caller, what the
// caller is allowed to do, and a way to say no. Each one used to be a type
// switch over all five, reaching into `.Session` and `.Event` — which is how
// discordgo ended up imported by packages that have nothing to do with
// Discord's wire format, and why swapping the library would have touched them.
//
// Implementations live next to the context types, which is the one place that
// is allowed to know what produced them.
type CommandContext interface {
	// GuildID is empty in a direct message.
	GuildID() string
	ChannelID() string

	// UserID and Username identify the caller. Both fall back to a sentinel
	// rather than an error: a missing user is worth logging as unknown, and
	// never worth failing a command over.
	UserID() string
	Username() string

	// MemberPermissions is the caller's effective permission bits in this
	// channel, which is roles and channel overwrites already resolved.
	MemberPermissions() (int64, error)

	// ReplyEphemeral answers the caller and nobody else. On a plain message,
	// where Discord offers no ephemeral reply, it falls back to the channel.
	ReplyEphemeral(msg string) error

	// CanReplyPrivately reports whether ReplyEphemeral reaches only the
	// caller. Some refusals are worth saying out loud and some are only worth
	// saying quietly: a disabled command should not put a notice in the
	// channel every time somebody trips over it.
	CanReplyPrivately() bool

	// AuditLogger persists who ran what, or nil where an invocation is
	// deliberately not audited.
	AuditLogger() Logger

	// Store is the guild datastore, or nil for contexts built without one.
	// Named Store rather than Storage because the context types carry a field
	// by that name already.
	Store() *storage.Storage
}

// ContextFromInvocation returns the invocation's context in its library-neutral
// form, or nil if it carries something this package did not build.
func ContextFromInvocation(inv *command.Invocation) CommandContext {
	if inv == nil || inv.Data == nil {
		return nil
	}
	if c, ok := inv.Data.(CommandContext); ok {
		return c
	}
	return nil
}

// unknownUser is what an invocation with no identifiable caller reports. It is
// deliberately not an error: the audit log would rather record that something
// ran than drop the row.
const (
	// UnknownUserID is reported when an invocation carries no identifiable
	// caller. Exported so a caller can tell "no permissions" from "no idea who
	// this is", which are different answers and deserve different treatment.
	UnknownUserID = "unknown"

	// UnknownUsername is the matching fallback for a display name.
	UnknownUsername = "Unknown"
)

// memberPermissions is the shape all five contexts share. A caller nobody
// could identify has no permissions to resolve, and asking anyway would spend
// a request to be told so.
func memberPermissions(api SessionAPI, who Invoker) (int64, error) {
	// What the invocation itself said, when it said anything. Discord computes
	// this for the channel the command was used in and sends it along, so it
	// is both authoritative and always present -- unlike the cache below,
	// which only knows members it has been told about.
	if who.PermissionsKnown {
		return who.Permissions, nil
	}
	if api == nil || who.UserID == "" || who.UserID == UnknownUserID {
		return 0, nil
	}
	return api.MemberPermissions(who.UserID, who.ChannelID)
}

// respondEphemeral is the shape the three interaction contexts share.
func respondEphemeral(r Responder, msg string) error {
	if r == nil {
		return nil
	}
	return r.RespondEmbed(&Embed{Description: msg}, true)
}

// replyInChannel is the fallback for the two contexts Discord offers no
// ephemeral reply for.
func replyInChannel(api SessionAPI, channelID, msg string) error {
	if api == nil {
		return nil
	}
	return api.SendChannelMessage(channelID, msg)
}

// --- SlashInteractionContext ---

func (c *SlashInteractionContext) GuildID() string         { return c.Invoker.GuildID }
func (c *SlashInteractionContext) ChannelID() string       { return c.Invoker.ChannelID }
func (c *SlashInteractionContext) UserID() string          { return c.Invoker.UserID }
func (c *SlashInteractionContext) Username() string        { return c.Invoker.Username }
func (c *SlashInteractionContext) AuditLogger() Logger     { return c.Logger }
func (c *SlashInteractionContext) Store() *storage.Storage { return c.Storage }
func (c *SlashInteractionContext) MemberPermissions() (int64, error) {
	return memberPermissions(c.API, c.Invoker)
}
func (c *SlashInteractionContext) CanReplyPrivately() bool { return c.Responder != nil }
func (c *SlashInteractionContext) ReplyEphemeral(msg string) error {
	return respondEphemeral(c.Responder, msg)
}

// --- ComponentInteractionContext ---

func (c *ComponentInteractionContext) GuildID() string         { return c.Invoker.GuildID }
func (c *ComponentInteractionContext) ChannelID() string       { return c.Invoker.ChannelID }
func (c *ComponentInteractionContext) UserID() string          { return c.Invoker.UserID }
func (c *ComponentInteractionContext) Username() string        { return c.Invoker.Username }
func (c *ComponentInteractionContext) AuditLogger() Logger     { return c.Logger }
func (c *ComponentInteractionContext) Store() *storage.Storage { return c.Storage }
func (c *ComponentInteractionContext) MemberPermissions() (int64, error) {
	return memberPermissions(c.API, c.Invoker)
}
func (c *ComponentInteractionContext) CanReplyPrivately() bool { return c.Responder != nil }
func (c *ComponentInteractionContext) ReplyEphemeral(msg string) error {
	return respondEphemeral(c.Responder, msg)
}

// CustomID identifies which component was used -- the button's own id, set
// when the message was built.
func (c *ComponentInteractionContext) CustomID() string { return c.ComponentID }

// --- MessageApplicationCommandContext ---

func (c *MessageApplicationCommandContext) GuildID() string   { return c.Invoker.GuildID }
func (c *MessageApplicationCommandContext) ChannelID() string { return c.Invoker.ChannelID }
func (c *MessageApplicationCommandContext) UserID() string    { return c.Invoker.UserID }
func (c *MessageApplicationCommandContext) Username() string  { return c.Invoker.Username }
func (c *MessageApplicationCommandContext) AuditLogger() Logger {
	return c.Logger
}
func (c *MessageApplicationCommandContext) Store() *storage.Storage { return c.Storage }
func (c *MessageApplicationCommandContext) MemberPermissions() (int64, error) {
	return memberPermissions(c.API, c.Invoker)
}
func (c *MessageApplicationCommandContext) CanReplyPrivately() bool { return c.Responder != nil }
func (c *MessageApplicationCommandContext) ReplyEphemeral(msg string) error {
	return respondEphemeral(c.Responder, msg)
}
