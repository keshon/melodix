package disgoreply

import (
	"io"
	"strings"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"

	"github.com/keshon/melodix/internal/discord/cmdadapter"
)

// EmbedColor is the default colour for an embed that did not choose one. It is
// the same value the discordgo backend uses, because it is the bot's colour
// rather than the library's.
const EmbedColor = 0xb01e66

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
// owed. Matched on text because the API returns it as a generic error.
func alreadyAcknowledged(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "already been acknowledged")
}

func (r *Responder) AckDeferred(ephemeral bool) error {
	err := r.event.DeferCreateMessage(ephemeral)
	if alreadyAcknowledged(err) {
		return nil
	}
	return err
}

func (r *Responder) RespondEmbed(embed *cmdadapter.Embed, ephemeral bool) error {
	err := r.event.CreateMessage(discord.MessageCreate{
		Embeds: Embeds(embed),
		Flags:  ephemeralFlags(ephemeral),
	})
	if err == nil {
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

func (r *Responder) FollowupEmbedMessage(embed *cmdadapter.Embed) (string, string, error) {
	msg, err := r.followup(discord.MessageCreate{Embeds: Embeds(embed)})
	if err != nil {
		return "", "", err
	}
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
	return r.component.UpdateMessage(discord.MessageUpdate{
		Embeds:     &[]discord.Embed{Embed(embed)},
		Components: &[]discord.LayoutComponent{},
	})
}

func (r *Responder) followup(create discord.MessageCreate) (*discord.Message, error) {
	return r.event.Client().Rest.CreateFollowupMessage(r.appID, r.token, create)
}

func (r *Responder) editResponse(update discord.MessageUpdate) error {
	_, err := r.event.Client().Rest.UpdateInteractionResponse(r.appID, r.token, update)
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
