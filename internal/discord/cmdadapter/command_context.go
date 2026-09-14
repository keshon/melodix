package cmdadapter

import (
	"github.com/bwmarrin/discordgo"
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

	unknownUsername = "Unknown"
)

// interactionUser reads the caller off an interaction. A guild interaction
// carries Member, a direct message carries User, and neither is guaranteed.
func interactionUser(e *discordgo.InteractionCreate) *discordgo.User {
	if e == nil {
		return nil
	}
	if e.Member != nil && e.Member.User != nil {
		return e.Member.User
	}
	return e.User
}

func userID(u *discordgo.User) string {
	if u == nil {
		return UnknownUserID
	}
	return u.ID
}

func username(u *discordgo.User) string {
	if u == nil {
		return unknownUsername
	}
	return u.Username
}

func channelPermissions(s *discordgo.Session, uID, channelID string) (int64, error) {
	if s == nil || uID == "" || uID == UnknownUserID {
		return 0, nil
	}
	return s.UserChannelPermissions(uID, channelID)
}

// respondEphemeral is the shape all three interaction contexts share.
func respondEphemeral(r Responder, s *discordgo.Session, e *discordgo.InteractionCreate, msg string) error {
	if r == nil {
		return nil
	}
	return r.RespondEmbedEphemeral(s, e, &Embed{Description: msg})
}

// --- SlashInteractionContext ---

func (c *SlashInteractionContext) GuildID() string         { return c.Event.GuildID }
func (c *SlashInteractionContext) ChannelID() string       { return c.Event.ChannelID }
func (c *SlashInteractionContext) UserID() string          { return userID(interactionUser(c.Event)) }
func (c *SlashInteractionContext) Username() string        { return username(interactionUser(c.Event)) }
func (c *SlashInteractionContext) AuditLogger() Logger     { return c.Logger }
func (c *SlashInteractionContext) Store() *storage.Storage { return c.Storage }
func (c *SlashInteractionContext) MemberPermissions() (int64, error) {
	return channelPermissions(c.Session, c.UserID(), c.ChannelID())
}
func (c *SlashInteractionContext) CanReplyPrivately() bool { return c.Responder != nil }
func (c *SlashInteractionContext) ReplyEphemeral(msg string) error {
	return respondEphemeral(c.Responder, c.Session, c.Event, msg)
}

// --- ComponentInteractionContext ---

func (c *ComponentInteractionContext) GuildID() string         { return c.Event.GuildID }
func (c *ComponentInteractionContext) ChannelID() string       { return c.Event.ChannelID }
func (c *ComponentInteractionContext) UserID() string          { return userID(interactionUser(c.Event)) }
func (c *ComponentInteractionContext) Username() string        { return username(interactionUser(c.Event)) }
func (c *ComponentInteractionContext) AuditLogger() Logger     { return c.Logger }
func (c *ComponentInteractionContext) Store() *storage.Storage { return c.Storage }
func (c *ComponentInteractionContext) MemberPermissions() (int64, error) {
	return channelPermissions(c.Session, c.UserID(), c.ChannelID())
}
func (c *ComponentInteractionContext) CanReplyPrivately() bool { return c.Responder != nil }
func (c *ComponentInteractionContext) ReplyEphemeral(msg string) error {
	return respondEphemeral(c.Responder, c.Session, c.Event, msg)
}

// --- MessageApplicationCommandContext ---

func (c *MessageApplicationCommandContext) GuildID() string   { return c.Event.GuildID }
func (c *MessageApplicationCommandContext) ChannelID() string { return c.Event.ChannelID }
func (c *MessageApplicationCommandContext) UserID() string    { return userID(interactionUser(c.Event)) }
func (c *MessageApplicationCommandContext) Username() string {
	return username(interactionUser(c.Event))
}
func (c *MessageApplicationCommandContext) AuditLogger() Logger     { return c.Logger }
func (c *MessageApplicationCommandContext) Store() *storage.Storage { return c.Storage }
func (c *MessageApplicationCommandContext) MemberPermissions() (int64, error) {
	return channelPermissions(c.Session, c.UserID(), c.ChannelID())
}
func (c *MessageApplicationCommandContext) CanReplyPrivately() bool { return c.Responder != nil }
func (c *MessageApplicationCommandContext) ReplyEphemeral(msg string) error {
	return respondEphemeral(c.Responder, c.Session, c.Event, msg)
}

// --- MessageContext ---

func (c *MessageContext) GuildID() string   { return c.Event.GuildID }
func (c *MessageContext) ChannelID() string { return c.Event.ChannelID }
func (c *MessageContext) UserID() string    { return userID(c.Event.Author) }
func (c *MessageContext) Username() string  { return username(c.Event.Author) }

// AuditLogger is nil on purpose: message commands are not written to the
// audit log, and a context that cannot reach a logger cannot start being.
func (c *MessageContext) AuditLogger() Logger     { return nil }
func (c *MessageContext) Store() *storage.Storage { return c.Storage }
func (c *MessageContext) MemberPermissions() (int64, error) {
	return channelPermissions(c.Session, c.UserID(), c.ChannelID())
}

// ReplyEphemeral has no ephemeral form here: a plain message command is
// answered in the channel it was sent in, where everyone can see it. Keeping
// the method means the caller does not have to care which it got.
func (c *MessageContext) CanReplyPrivately() bool { return false }
func (c *MessageContext) ReplyEphemeral(msg string) error {
	if c.Session == nil {
		return nil
	}
	_, err := c.Session.ChannelMessageSend(c.ChannelID(), msg)
	return err
}

// --- MessageReactionContext ---

func (c *MessageReactionContext) GuildID() string   { return c.Event.GuildID }
func (c *MessageReactionContext) ChannelID() string { return c.Event.ChannelID }
func (c *MessageReactionContext) UserID() string    { return c.Event.UserID }

// Username falls back to the user ID rather than a sentinel: a reaction does
// not always carry the member, and an audit row naming an ID is worth more
// than one naming nobody.
func (c *MessageReactionContext) Username() string {
	if c.Event.Member != nil && c.Event.Member.User != nil {
		return c.Event.Member.User.Username
	}
	return c.Event.UserID
}
func (c *MessageReactionContext) AuditLogger() Logger     { return c.Logger }
func (c *MessageReactionContext) Store() *storage.Storage { return c.Storage }
func (c *MessageReactionContext) MemberPermissions() (int64, error) {
	return channelPermissions(c.Session, c.UserID(), c.ChannelID())
}
func (c *MessageReactionContext) CanReplyPrivately() bool { return false }
func (c *MessageReactionContext) ReplyEphemeral(msg string) error {
	if c.Session == nil {
		return nil
	}
	_, err := c.Session.ChannelMessageSend(c.ChannelID(), msg)
	return err
}
