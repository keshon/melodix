package cmdadapter

import (
	"io"
	"time"
)

// Responder is everything a command does with the one interaction it was
// invoked from. It names no library, and it is built per interaction rather
// than shared: the thing it used to take as two parameters on every call --
// a session and an event -- is what an implementation now holds.
//
// That shape is not a preference. Under discordgo an interaction reply needs
// the session and the event together, and under disgo the event answers for
// itself; a single receiver is the only shape both can satisfy, and taking
// the pair as parameters is what made this interface discordgo's.
//
// ephemeral is a parameter rather than a second method because every caller
// that has one has both, and the pair of names doubled an interface that is
// already the widest thing here.
type Responder interface {
	AckDeferred(ephemeral bool) error
	RespondEmbed(embed *Embed, ephemeral bool) error

	// RespondText answers with plain content rather than an embed. The
	// difference is not cosmetic: content is capped at 2000 characters where
	// an embed description takes 4096, and a caller that sized its output to
	// one limit must not silently be given the other.
	RespondText(content string, ephemeral bool) error

	RespondEmbedWithFile(embed *Embed, r io.Reader, fileName string) error
	FollowupEmbed(embed *Embed, ephemeral bool) error

	// FollowupEmbedWithComponents answers a deferred interaction with controls
	// attached. It is ephemeral: only the caller should be able to press the
	// buttons of a chooser they asked for.
	FollowupEmbedWithComponents(embed *Embed, rows []ActionRow) error

	// AnswerEmbedMessage makes the embed the interaction's own answer and
	// reports where it landed, so a caller that means to edit it later can
	// find it again.
	//
	// The guild's music status message works this way: created from the
	// interaction that started playback, then edited for as long as the track
	// plays -- well past the token's expiry, so the later edits go through
	// the channel rather than the interaction.
	//
	// It answers rather than posting a followup on purpose. A followup leaves
	// the deferred placeholder spinning beside the message it just created,
	// which is two messages for one reply and one of them says nothing.
	AnswerEmbedMessage(embed *Embed) (channelID, messageID string, err error)

	// EditResponseText replaces the original reply with plain text, which is
	// the fallback when an embed could not be delivered.
	EditResponseText(content string) error

	// ReplaceMessage answers a component interaction by rewriting the message
	// it came from, which is how a chooser is consumed: the buttons go away
	// with the same click that acts on them, so nothing can be pressed twice.
	ReplaceMessage(embed *Embed) error

	// ResolveDeferred removes the "thinking" placeholder when an interaction
	// was deferred and then answered some other way.
	//
	// Deferring posts a visible placeholder, and only editing the original
	// response replaces it -- a followup adds a message beside it and leaves
	// it spinning, and so does editing a message the command owns. Several
	// commands answer exactly that way on purpose: /play reports into the
	// guild's music status message rather than replying, so its placeholder
	// had nothing to resolve it and sat there until Discord gave up on it.
	//
	// Calling this when the interaction was answered normally does nothing,
	// so the dispatcher can call it unconditionally.
	ResolveDeferred() error
}

// SessionAPI is what a command asks of the connection rather than of one
// interaction. It is separate from Responder because its answers outlive the
// interaction token, and because the asynchronous paths -- auto-advance,
// queue end -- have one of these and no interaction at all.
type SessionAPI interface {
	// MemberPermissions is a caller's effective permission bits in a channel,
	// which is roles and channel overwrites already resolved.
	MemberPermissions(userID, channelID string) (int64, error)

	// CheckBotPermissions reports whether the bot may manage messages in a
	// channel.
	CheckBotPermissions(channelID string) bool

	// CheckBotVoicePermissions reports whether the bot may connect and speak
	// in a voice channel. Asked before playback so a refusal is a message
	// rather than a silent failure to join.
	CheckBotVoicePermissions(channelID string) (bool, error)

	// SendChannelMessage posts plain content to a channel, which is how a
	// command with no interaction to answer says anything at all.
	SendChannelMessage(channelID, content string) error

	SendChannelEmbed(channelID string, embed *Embed) error

	// GuildInfo describes a guild. See GuildInfo for what the counts mean.
	GuildInfo(guildID string) (GuildInfo, error)

	// Latency is the round trip to the gateway, which is what a ping reports.
	Latency() time.Duration

	// EmbedColor is the default colour for an embed that did not choose one.
	EmbedColor() int
}

// BotAPI is SessionAPI plus what the bot itself needs and no command does.
//
// The split is deliberate: commands get the smaller surface, so a command
// cannot edit a message it did not post or go looking for who is in a voice
// channel. The voice service needs both, because the guild's music status
// message outlives the interaction that created it and has to be edited
// through the connection instead.
type BotAPI interface {
	SessionAPI

	// EditChannelEmbed replaces the embed on a message the bot posted
	// earlier, named by where it landed rather than by an interaction token
	// that has since expired.
	EditChannelEmbed(channelID, messageID string, embed *Embed) error

	// UserVoiceChannel is the voice channel a user is connected to, or an
	// error if they are not in one.
	UserVoiceChannel(guildID, userID string) (string, error)
}

// CommandSyncer registers a guild's slash commands with Discord.
type CommandSyncer interface {
	SyncGuildCommands(guildID string) error
}

// Logger persists command invocations (implemented by cmdlogger).
type Logger interface {
	LogCommand(guildID, channelID, userID, username, commandName string) error
}

// SlashProvider is implemented by commands that expose a slash definition.
type SlashProvider interface {
	SlashDefinition() *SlashCommand
}

// ContextMenuProvider is implemented by commands that expose a context-menu
// definition.
type ContextMenuProvider interface {
	ContextDefinition() *SlashCommand
}

// ComponentInteractionHandler is implemented by commands that handle message
// components (buttons/selects) whose customID matches the command name.
type ComponentInteractionHandler interface {
	Component(*ComponentInteractionContext) error
}

// Unlogged is implemented by commands that must never reach the audit log.
//
// The log records who ran what and when, which is ordinarily the point. The
// exception is a command whose worth depends on the caller staying unknown: a
// row naming them undoes that from the other side, however careful the command
// itself was, and an admin reading the log learns exactly what the command
// promised to hide. Opting out is therefore part of what such a command
// guarantees rather than a preference, which is why it is declared on the
// command instead of configured somewhere else.
//
// This command layer is shared verbatim with server-domme, where `/confess`
// relies on it. Keep the two in step rather than trimming this to whatever the
// local command set happens to need — a capability present in one and absent
// in the other turns every later edit to this package into a two-way merge.
type Unlogged interface {
	Unlogged()
}

// Meta is the read-side view of a command's classification, used by consumers
// that only group/filter commands (readme generation, middleware checks).
type Meta interface {
	Group() string
	Category() string
	UserPermissions() []int64
}

// Handler is the interface every melodix command implements; Meta is embedded
// so classification is defined in exactly one place.
type Handler interface {
	Meta
	Name() string
	Description() string
	Run(ctx interface{}) error
}
