package reply

import (
	"errors"
	"io"
	"sync"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"

	"github.com/keshon/melodix/internal/discord/cmdadapter"
)

// respondable is what both interaction events can do. Keeping it as an
// interface rather than two Responder types means the six reply methods are
// written once: the difference between a slash command and a component click
// is which of them can also rewrite the message it came from.
type respondable interface {
	CreateMessage(discord.MessageCreate, ...rest.RequestOpt) error
	DeferCreateMessage(ephemeral bool, opts ...rest.RequestOpt) error
	Client() *bot.Client
}

// Responder answers one interaction, and holds the event that answering it
// needs.
type Responder struct {
	event respondable
	// component is set only for a component interaction, which is the one
	// kind that can answer by rewriting the message it arrived on.
	component *events.ComponentInteractionCreate
	// appID and token address the interaction for followups and edits, which
	// go through REST rather than through the event.
	appID snowflake.ID
	token string

	// response decides whether the deferred placeholder is still owed
	// something. See responseState.
	response responseState
}

var _ cmdadapter.Responder = (*Responder)(nil)

// NewCommandResponder binds a slash or context-menu interaction.
func NewCommandResponder(e *events.ApplicationCommandInteractionCreate) *Responder {
	return &Responder{
		event: e,
		appID: e.ApplicationID(),
		token: e.Token(),
	}
}

// NewComponentResponder binds a component interaction.
func NewComponentResponder(e *events.ComponentInteractionCreate) *Responder {
	return &Responder{
		event:     e,
		component: e,
		appID:     e.ApplicationID(),
		token:     e.Token(),
	}
}

// ephemeralFlags is what makes a reply visible only to the caller.
func ephemeralFlags(ephemeral bool) discord.MessageFlags {
	if ephemeral {
		return discord.MessageFlagEphemeral
	}
	return 0
}

// alreadyAcknowledged reports whether an interaction had already been answered.
//
// The same recovery the discordgo backend does, for the same reason: a command
// that defers and then responds, or two paths that both answer, would
// otherwise surface as a failed reply rather than as the message the user was
// owed.
//
// It used to match on the message text, because discordgo surfaced this as a
// generic error and there was nothing else to match on. disgo types it, and
// the difference is not cosmetic: every response fallback in this file depends
// on recognising it, and the old match survived only because the rendered
// error happened to contain the English phrase Discord sends.
func alreadyAcknowledged(err error) bool {
	var restErr *rest.Error
	return errors.As(err, &restErr) &&
		restErr.Code == rest.JSONErrorCodeInteractionAlreadyAcknowledged
}

func (r *Responder) AckDeferred(ephemeral bool) error {
	err := r.event.DeferCreateMessage(ephemeral)
	if alreadyAcknowledged(err) {
		r.markDeferred()
		return nil
	}
	if err == nil {
		r.markDeferred()
	}
	return err
}

func (r *Responder) markDeferred() { r.response.deferredNow() }
func (r *Responder) markAnswered() { r.response.answeredNow() }

// ResolveDeferred removes a placeholder that nothing answered -- no reply, no
// edit, no followup. See responseState for what counts as answered.
func (r *Responder) ResolveDeferred() error {
	if !r.response.takePending() {
		return nil
	}
	return r.event.Client().Rest.DeleteInteractionResponse(r.appID, r.token)
}

// responseState tracks what became of an interaction's original response.
//
// Deferring posts a visible placeholder, and the question at the end of a
// command is whether the caller ever saw anything in its place. Three routes
// count as answering, and getting the set wrong breaks something either way:
//
//   - Replacing or editing the original response. Obvious.
//   - Posting a followup. Less obvious and the one that matters: Discord
//     stops showing the placeholder once a followup lands, which is why
//     /queue and a first /play always looked right. Treating a followup as
//     "not answered" and deleting the original takes an *ephemeral* followup
//     down with it, because the interaction response is what carries it --
//     the reply blinks in and vanishes.
//
// What is left is the one path that answers with none of the three: /play
// editing the guild's music status message, a channel message it owns. That
// placeholder really is orphaned, and it is the only one to remove.
//
// A command runs on one goroutine, but the mutex is cheap and the alternative
// is a rule about which goroutine may reply.
type responseState struct {
	mu       sync.Mutex
	deferred bool
	answered bool
}

func (s *responseState) deferredNow() {
	s.mu.Lock()
	s.deferred = true
	s.mu.Unlock()
}

func (s *responseState) answeredNow() {
	s.mu.Lock()
	s.answered = true
	s.mu.Unlock()
}

// takePending reports whether a placeholder is owed an answer, and claims it:
// a second call returns false, so the delete cannot be issued twice.
func (s *responseState) takePending() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.deferred || s.answered {
		return false
	}
	s.answered = true
	return true
}

func (r *Responder) RespondEmbed(embed *cmdadapter.Embed, ephemeral bool) error {
	err := r.event.CreateMessage(discord.MessageCreate{
		Embeds: Embeds(embed),
		Flags:  ephemeralFlags(ephemeral),
	})
	if err == nil {
		r.markAnswered()
		return nil
	}
	if alreadyAcknowledged(err) {
		// An already-public deferred response cannot be turned ephemeral by
		// editing it, so an ephemeral answer becomes an ephemeral followup.
		if ephemeral {
			return r.FollowupEmbed(embed, true)
		}
		return r.editResponse(discord.MessageUpdate{Embeds: &[]discord.Embed{Embed(embed)}})
	}
	return err
}

func (r *Responder) RespondText(content string, ephemeral bool) error {
	err := r.event.CreateMessage(discord.MessageCreate{
		Content: content,
		Flags:   ephemeralFlags(ephemeral),
	})
	if err == nil {
		r.markAnswered()
		return nil
	}
	if alreadyAcknowledged(err) {
		if ephemeral {
			_, ferr := r.followup(discord.MessageCreate{
				Content: content,
				Flags:   discord.MessageFlagEphemeral,
			})
			return ferr
		}
		return r.EditResponseText(content)
	}
	return err
}

func (r *Responder) RespondEmbedWithFile(embed *cmdadapter.Embed, src io.Reader, fileName string) error {
	create := discord.MessageCreate{
		Embeds: Embeds(embed),
		Flags:  discord.MessageFlagEphemeral,
		Files:  []*discord.File{discord.NewFile(fileName, "", src)},
	}
	err := r.event.CreateMessage(create)
	if err == nil {
		r.markAnswered()
		return nil
	}
	if alreadyAcknowledged(err) {
		_, ferr := r.followup(create)
		return ferr
	}
	return err
}

func (r *Responder) FollowupEmbed(embed *cmdadapter.Embed, ephemeral bool) error {
	_, err := r.followup(discord.MessageCreate{
		Embeds: Embeds(embed),
		Flags:  ephemeralFlags(ephemeral),
	})
	return err
}

func (r *Responder) FollowupEmbedWithComponents(embed *cmdadapter.Embed, rows []cmdadapter.ActionRow) error {
	_, err := r.followup(discord.MessageCreate{
		Embeds:     Embeds(embed),
		Components: Components(rows),
		Flags:      discord.MessageFlagEphemeral,
	})
	return err
}

// AnswerEmbedMessage replaces the deferred placeholder with the embed, rather
// than posting a followup beside it. See cmdadapter.Responder.
func (r *Responder) AnswerEmbedMessage(embed *cmdadapter.Embed) (string, string, error) {
	msg, err := r.event.Client().Rest.UpdateInteractionResponse(r.appID, r.token,
		discord.MessageUpdate{Embeds: &[]discord.Embed{Embed(embed)}})
	if err != nil {
		return "", "", err
	}
	r.markAnswered()
	if msg == nil {
		return "", "", nil
	}
	return msg.ChannelID.String(), msg.ID.String(), nil
}

func (r *Responder) EditResponseText(content string) error {
	return r.editResponse(discord.MessageUpdate{Content: &content})
}

// ReplaceMessage rewrites the message a component arrived on, which is how a
// chooser is consumed: the buttons go away with the same click that acts on
// them, so nothing can be pressed twice. The empty component slice is the
// removal and has to be sent rather than omitted.
func (r *Responder) ReplaceMessage(embed *cmdadapter.Embed) error {
	if r.component == nil {
		// Not a component interaction; the nearest honest thing is a plain
		// answer rather than silently doing nothing.
		return r.RespondEmbed(embed, false)
	}
	err := r.component.UpdateMessage(discord.MessageUpdate{
		Embeds:     &[]discord.Embed{Embed(embed)},
		Components: &[]discord.LayoutComponent{},
	})
	if err == nil {
		r.markAnswered()
	}
	return err
}

func (r *Responder) followup(create discord.MessageCreate) (*discord.Message, error) {
	msg, err := r.event.Client().Rest.CreateFollowupMessage(r.appID, r.token, create)
	if err == nil {
		// The caller saw a reply, so the placeholder is spent. Deleting the
		// original response now would take an ephemeral followup with it.
		r.markAnswered()
	}
	return msg, err
}

func (r *Responder) editResponse(update discord.MessageUpdate) error {
	_, err := r.event.Client().Rest.UpdateInteractionResponse(r.appID, r.token, update)
	if err == nil {
		r.markAnswered()
	}
	return err
}

// API answers what a command asks of the connection rather than of one
// interaction. One of these is good for a whole session.
type API struct {
	client *bot.Client
}

// NewSessionAPI wraps a disgo client in the neutral surface.
func NewSessionAPI(client *bot.Client) *API {
	return &API{client: client}
}

var (
	_ cmdadapter.SessionAPI = (*API)(nil)
	_ cmdadapter.BotAPI     = (*API)(nil)
)

func (a *API) EmbedColor() int { return EmbedColor }

func (a *API) Latency() time.Duration {
	if a.client == nil || a.client.Gateway == nil {
		return 0
	}
	return a.client.Gateway.Latency()
}
