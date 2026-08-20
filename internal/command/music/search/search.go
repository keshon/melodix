package search

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/keshon/melodix/internal/command/music/common"
	"github.com/keshon/melodix/internal/command/music/playback"
	"github.com/keshon/melodix/internal/discord"
	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/discord/reply"
	"github.com/keshon/melodix/pkg/music/sources/youtube"
)

// resultCount is how many hits the chooser offers. Five is one Discord action
// row, so the buttons never wrap and there is nothing to paginate.
const resultCount = 5

// componentPrefix namespaces this command's button ids. The dispatcher in
// internal/discord routes a customID to the command whose name it starts with,
// so this must stay equal to Name().
const componentPrefix = "search"

// A button id is "search:<source>:<payload>" and is a wire format, not an
// internal detail: choosers already posted keep living in channels, and their
// ids come back whenever someone presses a button. So the source travels in the
// id from the very first version, even though only YouTube is offered today —
// adding SoundCloud later is then a new case here rather than a format change
// that silently mis-routes every chooser still on screen.
const (
	sourceYouTube = "yt"

	// customIDLimit is Discord's cap on a component id. YouTube ids are fixed
	// at 11 characters so the budget is never close, but a source whose payload
	// is a URL could exceed it, and a truncated id would come back unroutable.
	customIDLimit = 100
)

func buttonID(source, payload string) (string, bool) {
	id := componentPrefix + ":" + source + ":" + payload
	return id, len(id) <= customIDLimit
}

// The dispatcher reaches the click handler through this interface; asserting it
// here turns a signature drift into a build failure rather than buttons that
// quietly stop responding.
var _ cmdadapter.ComponentInteractionHandler = (*Search)(nil)

// Search offers a pick-one chooser for a query instead of /play's
// take-the-first-hit. It is YouTube-only: the other sources have no ranked
// search to choose from.
type Search struct {
	Bot      discord.VoiceAPI
	searcher *youtube.Searcher
}

func (c *Search) Name() string             { return componentPrefix }
func (c *Search) Description() string      { return "Search YouTube and pick a track to play" }
func (c *Search) Group() string            { return "music" }
func (c *Search) Category() string         { return "🎵 Music" }
func (c *Search) UserPermissions() []int64 { return []int64{} }

func (c *Search) SlashDefinition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        c.Name(),
		Description: c.Description(),
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "query",
				Description: "What to search for on YouTube",
				Required:    true,
			},
		},
	}
}

func (c *Search) Run(ctx interface{}) error {
	slashCtx, ok := ctx.(*cmdadapter.SlashInteractionContext)
	if !ok {
		return nil
	}
	s := slashCtx.Session
	e := slashCtx.Event

	var query string
	for _, opt := range e.ApplicationCommandData().Options {
		if opt.Name == "query" {
			query = strings.TrimSpace(opt.StringValue())
		}
	}
	if query == "" {
		return reply.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 Error",
			Description: "A search query is required.",
		})
	}

	// Ephemeral throughout: the chooser belongs to whoever asked, and only they
	// should be able to press its buttons.
	if err := reply.RespondDeferredEphemeral(s, e); err != nil {
		return fmt.Errorf("failed to send deferred response: %w", err)
	}

	hits, err := c.results().Search(query, resultCount)
	if err != nil {
		reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🔎 Search",
			Description: fmt.Sprintf("Nothing found for %q.", query),
			Color:       reply.EmbedColor,
		})
		return nil
	}

	lines := make([]string, 0, len(hits))
	buttons := make([]discordgo.MessageComponent, 0, len(hits))
	for _, h := range hits {
		// The pick travels entirely in the button id, so choosing needs no
		// server-side memory of what was offered: the chooser survives a bot
		// restart, and two people searching at once cannot interfere.
		id, ok := buttonID(sourceYouTube, h.VideoID)
		if !ok {
			continue
		}
		pos := len(lines) + 1
		lines = append(lines, common.FormatSearchLine(pos, h.Title, h.URL(), h.Author, h.Duration))
		buttons = append(buttons, discordgo.Button{
			Label:    fmt.Sprintf("%d", pos),
			Style:    discordgo.SecondaryButton,
			CustomID: id,
		})
	}
	if len(buttons) == 0 {
		reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🔎 Search",
			Description: fmt.Sprintf("Nothing playable found for %q.", query),
			Color:       reply.EmbedColor,
		})
		return nil
	}

	embed := &discordgo.MessageEmbed{
		Title:       "🔎 Search results",
		Description: strings.Join(lines, "\n"),
		Color:       reply.EmbedColor,
		Footer:      &discordgo.MessageEmbedFooter{Text: "Pick a number to queue it"},
	}
	return reply.FollowupEmbedEphemeralWithComponents(s, e, embed,
		[]discordgo.MessageComponent{discordgo.ActionsRow{Components: buttons}})
}

// Component handles a click on one of the chooser's buttons.
func (c *Search) Component(compCtx *cmdadapter.ComponentInteractionContext) error {
	s := compCtx.Session
	e := compCtx.Event

	source, payload, ok := parseButtonID(e.MessageComponentData().CustomID)
	if !ok {
		return nil
	}
	url, ok := trackURL(source, payload)
	if !ok {
		// A chooser from a future version, or a hand-crafted id.
		return reply.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🔎 Search",
			Description: "This result is from a version of the bot that is no longer running. Run `/search` again.",
			Color:       reply.EmbedColor,
		})
	}

	// Rewriting the chooser both acknowledges the click and takes the buttons
	// away, so a result cannot be queued twice by pressing again.
	if err := reply.ReplaceComponentMessage(s, e, &discordgo.MessageEmbed{
		Title:       "🔎 Search",
		Description: "Adding to the queue…",
		Color:       reply.EmbedColor,
	}); err != nil {
		return fmt.Errorf("failed to acknowledge selection: %w", err)
	}

	target, ok := playback.Join(c.Bot, s, e)
	if !ok {
		return nil
	}

	tracks, err := c.Bot.ResolveTracks(target.GuildID, url, "", "")
	if err != nil || len(tracks) == 0 {
		reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 Error",
			Description: fmt.Sprintf("Failed to resolve track: %v", err),
		})
		return nil
	}
	if err := target.Player.EnqueueTrackInfos(tracks); err != nil {
		playback.QueueError(s, e, err)
		return nil
	}

	playback.StartAndRender(c.Bot, s, e, compCtx.AppLog, target)
	return nil
}

// parseButtonID splits "search:<source>:<payload>".
func parseButtonID(customID string) (source, payload string, ok bool) {
	rest, found := strings.CutPrefix(customID, componentPrefix+":")
	if !found {
		return "", "", false
	}
	source, payload, found = strings.Cut(rest, ":")
	if !found || source == "" || payload == "" {
		return "", "", false
	}
	return source, payload, true
}

// trackURL turns a button payload back into a resolvable URL.
func trackURL(source, payload string) (string, bool) {
	switch source {
	case sourceYouTube:
		return "https://www.youtube.com/watch?v=" + payload, true
	default:
		return "", false
	}
}

// results lazily builds the searcher so a zero-value Search is usable.
func (c *Search) results() *youtube.Searcher {
	if c.searcher == nil {
		c.searcher = youtube.NewSearcher()
	}
	return c.searcher
}
