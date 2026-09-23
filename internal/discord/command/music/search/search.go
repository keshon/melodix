package search

import (
	"fmt"
	"strings"

	"github.com/keshon/melodix/internal/discord"
	"github.com/keshon/melodix/internal/discord/adapter"
	"github.com/keshon/melodix/internal/discord/command/music/playback"
	"github.com/keshon/melodix/internal/discord/command/music/tracklist"
	"github.com/keshon/melodix/internal/discord/reply"
	"github.com/keshon/melodix/internal/music"
	"github.com/keshon/melodix/pkg/music/sources"
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
// ids come back whenever someone presses a button. The source travels in the id
// so that adding one is a new case here rather than a format change that
// silently mis-routes every chooser still on screen — which is exactly what
// carrying it from the first version, when only YouTube was searchable, bought.
//
// The payload is the source's own compact id, never a URL: SoundCloud
// permalinks run past 130 characters and 8 in 100 already exceed the budget
// below, so a URL-shaped payload would silently drop results from the chooser.
const (
	sourceYouTube    = "yt"
	sourceSoundCloud = "sc"

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
var _ adapter.ComponentInteractionHandler = (*Command)(nil)

// Command is /search: a pick-one chooser for a query instead of /play's
// take-the-first-hit. Radio is absent on purpose: a stream has nothing to rank.
type Command struct {
	Bot discord.VoiceAPI
}

func (c *Command) Name() string             { return componentPrefix }
func (c *Command) Description() string      { return "Search and pick a track to play" }
func (c *Command) Group() string            { return "music" }
func (c *Command) Category() string         { return "🎵 Music" }
func (c *Command) UserPermissions() []int64 { return []int64{} }

func (c *Command) SlashDefinition() *adapter.SlashCommand {
	return &adapter.SlashCommand{
		Name:        c.Name(),
		Description: c.Description(),
		Options: []adapter.SlashOption{
			{
				Type:        adapter.OptionString,
				Name:        "query",
				Description: "What to search for",
				Required:    true,
			},
			{
				Type:        adapter.OptionString,
				Name:        "source",
				Description: "Where to search (YouTube by default)",
				Choices: []adapter.SlashChoice{
					{Name: "YouTube", Value: sources.YouTube},
					{Name: "SoundCloud", Value: sources.SoundCloud},
				},
			},
		},
	}
}

func (c *Command) Run(slashCtx *adapter.SlashInteractionContext) error {
	query := strings.TrimSpace(slashCtx.StringOption("query"))
	wanted := slashCtx.StringOption("source")
	tag, ok := tagOf(wanted)
	if !ok {
		return slashCtx.RespondEphemeral(&adapter.Embed{
			Title:       "🎵 Error",
			Description: fmt.Sprintf("%s cannot be searched", wanted),
		})
	}
	if query == "" {
		return slashCtx.RespondEphemeral(&adapter.Embed{
			Title:       "🎵 Error",
			Description: "A search query is required.",
		})
	}

	// Ephemeral throughout: the chooser belongs to whoever asked, and only they
	// should be able to press its buttons.
	if err := slashCtx.DeferEphemeral(); err != nil {
		return fmt.Errorf("failed to send deferred response: %w", err)
	}

	hits, err := c.Bot.Music().Search(wanted, query, resultCount)
	if err != nil {
		slashCtx.FollowupEphemeral(&adapter.Embed{
			Title:       "🔎 Search",
			Description: fmt.Sprintf("Nothing found for %q.", query),
			Color:       reply.EmbedColor,
		})
		return nil
	}

	lines := make([]string, 0, len(hits))
	buttons := make([]adapter.Button, 0, len(hits))
	for _, h := range hits {
		// The pick travels entirely in the button id, so choosing needs no
		// server-side memory of what was offered: the chooser survives a bot
		// restart, and two people searching at once cannot interfere.
		id, ok := buttonID(tag, h.ID)
		if !ok {
			continue
		}
		pos := len(lines) + 1
		lines = append(lines, tracklist.SearchLine(pos, h.Title, h.URL, h.Author, h.Duration))
		buttons = append(buttons, adapter.Button{
			Label:    fmt.Sprintf("%d", pos),
			CustomID: id,
		})
	}
	if len(buttons) == 0 {
		slashCtx.FollowupEphemeral(&adapter.Embed{
			Title:       "🔎 Search",
			Description: fmt.Sprintf("Nothing playable found for %q.", query),
			Color:       reply.EmbedColor,
		})
		return nil
	}

	embed := &adapter.Embed{
		Title:       "🔎 Search results",
		Description: strings.Join(lines, "\n"),
		Color:       reply.EmbedColor,
		Footer:      "Pick a number to queue it",
	}
	return slashCtx.FollowupWith(adapter.Reply{
		Embed: embed, Ephemeral: true,
		Buttons: []adapter.ActionRow{{Buttons: buttons}},
	})
}

// Component handles a click on one of the chooser's buttons.
func (c *Command) Component(compCtx *adapter.ComponentInteractionContext) error {
	tag, payload, ok := parseButtonID(compCtx.CustomID())
	if !ok {
		return nil
	}
	source, ok := sourceOf(tag)
	if !ok {
		// A chooser from a future version, or a hand-crafted id.
		return compCtx.RespondEphemeral(&adapter.Embed{
			Title:       "🔎 Search",
			Description: "This result is from a version of the bot that is no longer running. Run `/search` again.",
			Color:       reply.EmbedColor,
		})
	}

	// Rewriting the chooser both acknowledges the click and takes the buttons
	// away, so a result cannot be queued twice by pressing again.
	if err := compCtx.ReplaceMessage(&adapter.Embed{
		Title:       "🔎 Search",
		Description: "Adding to the queue…",
		Color:       reply.EmbedColor,
	}); err != nil {
		return fmt.Errorf("failed to acknowledge selection: %w", err)
	}

	target, ok := playback.Join(c.Bot, compCtx)
	if !ok {
		return nil
	}

	url, err := c.Bot.Music().HitURL(source, payload)
	if err != nil {
		compCtx.FollowupEphemeral(&adapter.Embed{
			Title:       "🎵 Error",
			Description: fmt.Sprintf("Could not look that track up again: %v", err),
		})
		return nil
	}

	in := music.Input{Kind: music.InputQuery, Query: url}
	added, err := c.Bot.Music().Add(target.GuildID, in, "", "")
	if err != nil {
		playback.AddError(compCtx, err)
		return nil
	}

	playback.StartAndRender(c.Bot, compCtx, compCtx.AppLog, target, added)
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

// tagOf is the button tag for a search source option; the empty option is
// YouTube. ok is false for a source that cannot be searched.
func tagOf(source string) (tag string, ok bool) {
	switch source {
	case "", sources.YouTube:
		return sourceYouTube, true
	case sources.SoundCloud:
		return sourceSoundCloud, true
	default:
		return "", false
	}
}

// sourceOf is the source a button tag stands for. ok is false for a tag this
// build does not know -- a chooser from a future version, or a hand-crafted
// id -- which must fail closed rather than be resolved as some default.
func sourceOf(tag string) (source string, ok bool) {
	switch tag {
	case sourceYouTube:
		return sources.YouTube, true
	case sourceSoundCloud:
		return sources.SoundCloud, true
	default:
		return "", false
	}
}
