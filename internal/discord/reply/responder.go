package reply

import (
	"errors"
	"sync"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"

	"github.com/keshon/melodix/internal/discord/adapter"
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

// interactionREST is the part of disgo's REST client a Responder addresses by
// application id and token: followups, and the original response once it
// exists. An interface so the order of those calls can be tested.
type interactionREST interface {
	CreateFollowupMessage(applicationID snowflake.ID, interactionToken string, messageCreate discord.MessageCreate, opts ...rest.RequestOpt) (*discord.Message, error)
	UpdateInteractionResponse(applicationID snowflake.ID, interactionToken string, messageUpdate discord.MessageUpdate, opts ...rest.RequestOpt) (*discord.Message, error)
	DeleteInteractionResponse(applicationID snowflake.ID, interactionToken string, opts ...rest.RequestOpt) error
}

// Responder answers one interaction, and holds the event that answering it
// needs.
type Responder struct {
	event respondable
	rest  interactionREST
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

var _ adapter.Responder = (*Responder)(nil)

// NewCommandResponder binds a slash or context-menu interaction.
func NewCommandResponder(e *events.ApplicationCommandInteractionCreate) *Responder {
	return &Responder{
		event: e,
		rest:  e.Client().Rest,
		appID: e.ApplicationID(),
		token: e.Token(),
	}
}

// NewComponentResponder binds a component interaction.
func NewComponentResponder(e *events.ComponentInteractionCreate) *Responder {
	return &Responder{
		event:     e,
		rest:      e.Client().Rest,
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
		r.markDeferred(ephemeral)
		return nil
	}
	if err == nil {
		r.markDeferred(ephemeral)
	}
	return err
}

func (r *Responder) markDeferred(ephemeral bool) { r.response.deferredNow(ephemeral) }
func (r *Responder) markAnswered()               { r.response.answeredNow() }

// ResolveDeferred removes a placeholder that nothing answered -- no reply, no
// edit, no followup. See responseState for what counts as answered.
func (r *Responder) ResolveDeferred() error {
	if !r.response.takePending() {
		return nil
	}
	return r.rest.DeleteInteractionResponse(r.appID, r.token)
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
	// private is whether the deferral was ephemeral, which decides who sees
	// whatever first replaces the placeholder. See takePublicPlaceholder.
	private  bool
	answered bool
}

func (s *responseState) deferredNow(ephemeral bool) {
	s.mu.Lock()
	s.deferred = true
	s.private = ephemeral
	s.mu.Unlock()
}

func (s *responseState) answeredNow() {
	s.mu.Lock()
	s.answered = true
	s.mu.Unlock()
}

// takePublicPlaceholder reports whether an ephemeral reply is about to be
// posted where everyone will see it, and claims the placeholder if so.
//
// The first followup after a deferral replaces the "thinking" placeholder and
// takes the deferral's visibility, not its own. So after a public deferral an
// ephemeral followup was public: /play's voice errors, /next's, /stop's and the
// dispatcher's own error replies all went to the whole channel. Deleting the
// public placeholder first leaves the followup to stand on its own flags. The
// order is the point -- deleting it afterwards would take the followup, which
// by then is the original response, down with it.
func (s *responseState) takePublicPlaceholder() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.deferred || s.private || s.answered {
		return false
	}
	s.answered = true
	return true
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

// create turns a Reply into the message disgo sends. One place decides what a
// reply's fields mean, so Respond and Followup cannot disagree about it.
func create(rep adapter.Reply) discord.MessageCreate {
	msg := discord.MessageCreate{
		Content: rep.Text,
		Flags:   ephemeralFlags(rep.Ephemeral),
	}
	if rep.Embed != nil {
		msg.Embeds = Embeds(rep.Embed)
	}
	if rep.File != nil {
		msg.Files = []*discord.File{discord.NewFile(rep.FileName, "", rep.File)}
	}
	if len(rep.Buttons) > 0 {
		msg.Components = Components(rep.Buttons)
	}
	return msg
}

// Respond answers the interaction itself.
//
// The recovery below is why this is worth writing once. An interaction that
// has already been acknowledged cannot be answered again, and what to do
// instead depends on what was asked for: an ephemeral reply becomes an
// ephemeral followup, because a response that went out public cannot be made
// private by editing it; a public one edits the response that is already
// there. Getting that wrong shows the wrong people the reply, and it used to
// be written out separately for each shape of message.
func (r *Responder) Respond(rep adapter.Reply) error {
	msg := create(rep)
	err := r.event.CreateMessage(msg)
	if err == nil {
		r.markAnswered()
		return nil
	}
	if !alreadyAcknowledged(err) {
		return err
	}
	if rep.Ephemeral || rep.File != nil || len(rep.Buttons) > 0 {
		_, ferr := r.followup(msg)
		return ferr
	}
	if rep.Embed != nil {
		return r.editResponse(discord.MessageUpdate{Embeds: &[]discord.Embed{Embed(rep.Embed)}})
	}
	return r.EditResponseText(rep.Text)
}

// Followup posts beside an answer already given.
func (r *Responder) Followup(rep adapter.Reply) error {
	_, err := r.followup(create(rep))
	return err
}

// AnswerEmbedMessage replaces the deferred placeholder with the embed, rather
// than posting a followup beside it. See adapter.Responder.
func (r *Responder) AnswerEmbedMessage(embed *adapter.Embed) (string, string, error) {
	msg, err := r.rest.UpdateInteractionResponse(r.appID, r.token,
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
func (r *Responder) ReplaceMessage(rep adapter.Reply) error {
	if r.component == nil {
		// Not a component interaction; the nearest honest thing is a plain
		// answer rather than silently doing nothing.
		return r.Respond(rep)
	}
	embeds := []discord.Embed{}
	if rep.Embed != nil {
		embeds = []discord.Embed{Embed(rep.Embed)}
	}
	update := discord.MessageUpdate{
		Embeds: &embeds,
	}
	if rep.Text != "" {
		update.Content = &rep.Text
	}
	if rep.Buttons == nil {
		update.Components = &[]discord.LayoutComponent{}
	} else {
		comps := Components(rep.Buttons)
		update.Components = &comps
	}
	err := r.component.UpdateMessage(update)
	if err == nil {
		r.markAnswered()
	}
	return err
}

func (r *Responder) followup(create discord.MessageCreate) (*discord.Message, error) {
	if create.Flags.Has(discord.MessageFlagEphemeral) && r.response.takePublicPlaceholder() {
		// A failed delete still leaves a reply worth sending; it is public,
		// which is the old behaviour rather than a lost message.
		_ = r.rest.DeleteInteractionResponse(r.appID, r.token)
	}
	msg, err := r.rest.CreateFollowupMessage(r.appID, r.token, create)
	if err == nil {
		// The caller saw a reply, so the placeholder is spent. Deleting the
		// original response now would take an ephemeral followup with it.
		r.markAnswered()
	}
	return msg, err
}

func (r *Responder) editResponse(update discord.MessageUpdate) error {
	_, err := r.rest.UpdateInteractionResponse(r.appID, r.token, update)
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
	_ adapter.SessionAPI = (*API)(nil)
	_ adapter.BotAPI     = (*API)(nil)
)

func (a *API) EmbedColor() int { return EmbedColor }

func (a *API) Latency() time.Duration {
	if a.client == nil || a.client.Gateway == nil {
		return 0
	}
	return a.client.Gateway.Latency()
}
