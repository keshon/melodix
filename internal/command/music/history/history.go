package history

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/keshon/melodix/internal/command/music/common"
	"github.com/keshon/melodix/internal/discord"
	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/discord/reply"
	"github.com/keshon/melodix/internal/domain"
)

type History struct {
	Bot discord.VoiceAPI
}

func (c *History) Name() string { return "history" }
func (c *History) Description() string {
	return "Show recently played tracks (replay by id with /play)"
}
func (c *History) Group() string            { return "music" }
func (c *History) Category() string         { return "🎵 Music" }
func (c *History) UserPermissions() []int64 { return []int64{} }

// discordgo requires a pointer for MinValue on slash options.
var historyPageMinValue = 1.0

func (c *History) SlashDefinition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        c.Name(),
		Description: c.Description(),
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "view",
				Description: "Chronological list or plays per link",
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "Timeline", Value: "timeline"},
					{Name: "By URL", Value: "counts"},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "page",
				Description: "Page number (default 1)",
				Required:    false,
				MinValue:    &historyPageMinValue,
			},
		},
	}
}

const historyLinesPerPage = 15

const historyFooterReplay = "replay with `/play <id>`."

func (c *History) Run(ctx interface{}) error {
	slashCtx, ok := ctx.(*cmdadapter.SlashInteractionContext)
	if !ok {
		return nil
	}

	s := slashCtx.Session
	e := slashCtx.Event
	store := slashCtx.Storage

	var view = "timeline"
	var page int64 = 1
	for _, opt := range e.ApplicationCommandData().Options {
		switch opt.Name {
		case "view":
			if v := strings.TrimSpace(opt.StringValue()); v != "" {
				view = v
			}
		case "page":
			page = opt.IntValue()
		}
	}

	if err := s.InteractionRespond(e.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	}); err != nil {
		return fmt.Errorf("failed to send deferred response: %w", err)
	}

	guildID := e.GuildID
	if c.Bot.GetOrCreatePlayer(guildID) == nil {
		reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 Error",
			Description: "Music service is not available.",
		})
		return nil
	}

	if store == nil {
		reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 Error",
			Description: "Music history storage is not available.",
		})
		return nil
	}

	rows, err := store.ListMusicPlaybackTimeline(guildID)
	if err != nil {
		reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 History",
			Description: fmt.Sprintf("Could not load history: %v", err),
		})
		return nil
	}

	if len(rows) == 0 {
		reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 History",
			Description: "No playback history yet. Use `/play` first. History is stored per server; very old entries may be removed when the list is trimmed.",
			Color:       reply.EmbedColor,
		})
		return nil
	}

	view = strings.ToLower(strings.TrimSpace(view))
	if view == "" {
		view = "timeline"
	}

	var lines []string
	var totalRows int
	var embedTitle string
	var footerExtra string

	switch view {
	case "counts":
		counts := domain.AggregatePlaybackCounts(rows)
		totalRows = len(counts)
		embedTitle = "🎵 Playback history (by URL)"
		footerExtra = historyFooterReplay
		for _, r := range counts {
			lines = append(lines, common.FormatCountsLine(r))
		}
	default:
		totalRows = len(rows)
		embedTitle = "🎵 Playback history (timeline)"
		footerExtra = "Chronological; " + historyFooterReplay
		for _, m := range rows {
			lines = append(lines, common.FormatTimelineLine(m))
		}
	}

	totalPages := (totalRows + historyLinesPerPage - 1) / historyLinesPerPage
	if totalPages < 1 {
		totalPages = 1
	}
	if page < 1 {
		page = 1
	}
	if int64(totalPages) > 0 && page > int64(totalPages) {
		page = int64(totalPages)
	}

	start := int((page - 1) * int64(historyLinesPerPage))
	if start >= len(lines) {
		start = 0
		page = 1
	}
	end := start + historyLinesPerPage
	if end > len(lines) {
		end = len(lines)
	}

	var b strings.Builder
	for _, line := range lines[start:end] {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	desc := strings.TrimSpace(b.String())
	if len(desc) > 4000 {
		desc = desc[:3997] + "..."
	}

	embed := &discordgo.MessageEmbed{
		Title:       embedTitle,
		Description: desc,
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Page %d/%d (%d rows). %s", page, totalPages, totalRows, footerExtra),
		},
		Color: reply.EmbedColor,
	}
	if err := reply.FollowupEmbed(s, e, embed); err != nil {
		slashCtx.AppLog.Warn().Str("command", "history").Err(err).Msg("followup_embed_failed")
	}
	return nil
}
