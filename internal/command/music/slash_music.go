package music

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/keshon/melodix/internal/command"
	"github.com/keshon/melodix/internal/discord"
	"github.com/keshon/melodix/internal/domain"
	"github.com/keshon/melodix/internal/storage"
	"github.com/keshon/melodix/pkg/music/player"

	"github.com/bwmarrin/discordgo"
)

type MusicCommand struct {
	Bot discord.BotVoice
}

// discordgo requires a pointer for MinValue on slash options.
var historyPageMinValue = 1.0

func (c *MusicCommand) Name() string             { return "music" }
func (c *MusicCommand) Description() string      { return "Control music playback" }
func (c *MusicCommand) Group() string            { return "music" }
func (c *MusicCommand) Category() string         { return "🎵 Music" }
func (c *MusicCommand) UserPermissions() []int64 { return []int64{} }

func (c *MusicCommand) SlashDefinition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        c.Name(),
		Description: c.Description(),
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "play",
				Description: "Play a music track",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "input",
						Description: "Link or search query",
						Required:    true,
					},
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "source",
						Description: "Specify a source if search query is used",
						Choices: []*discordgo.ApplicationCommandOptionChoice{
							{Name: "YouTube", Value: "youtube"},
							{Name: "SoundCloud", Value: "soundcloud"},
							{Name: "Radio", Value: "radio"},
						},
					},
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "parser",
						Description: "Override autodetect parser",
						Choices: []*discordgo.ApplicationCommandOptionChoice{
							{Name: "ytdlp pipe", Value: "ytdlp-pipe"},
							{Name: "ytdlp link", Value: "ytdlp-link"},
							{Name: "kkdai pipe", Value: "kkdai-pipe"},
							{Name: "kkdai link", Value: "kkdai-link"},
							{Name: "ffmpeg direct link", Value: "ffmpeg-link"},
						},
					},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "next",
				Description: "Skip to the next track",
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "stop",
				Description: "Stop playback and clear queue",
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "history",
				Description: "Show recently played tracks (replay by id with /music play)",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "mode",
						Description: "Timeline (chronological) or counts grouped by URL",
						Required:    false,
						Choices: []*discordgo.ApplicationCommandOptionChoice{
							{Name: "Timeline (chronological)", Value: "timeline"},
							{Name: "Counts (by URL)", Value: "counts"},
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
			},
		},
	}
}

func (c *MusicCommand) Run(ctx interface{}) error {
	slashCtx, ok := ctx.(*command.SlashInteractionContext)
	if !ok {
		return nil
	}

	s := slashCtx.Session
	e := slashCtx.Event
	store := slashCtx.Storage

	if len(e.ApplicationCommandData().Options) == 0 {
		return discord.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Description: "Missing subcommand.",
		})
	}

	sub := e.ApplicationCommandData().Options[0]

	switch sub.Name {
	case "play":
		var input, source, parser string
		for _, opt := range sub.Options {
			switch opt.Name {
			case "input":
				input = opt.StringValue()
			case "source":
				source = opt.StringValue()
			case "parser":
				parser = opt.StringValue()
			}
		}
		return c.runPlay(s, e, input, source, parser, store)

	case "next":
		return c.runNext(s, e)

	case "stop":
		return c.runStop(s, e)

	case "history":
		mode := "timeline"
		var page int64 = 1
		for _, opt := range sub.Options {
			switch opt.Name {
			case "mode":
				if v := strings.TrimSpace(opt.StringValue()); v != "" {
					mode = v
				}
			case "page":
				page = opt.IntValue()
			}
		}
		return c.runHistory(s, e, page, mode, store)

	default:
		return discord.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Description: fmt.Sprintf("Unknown subcommand: %s", sub.Name),
		})
	}
}

func (c *MusicCommand) runPlay(s *discordgo.Session, e *discordgo.InteractionCreate, input, src, parser string, store *storage.Storage) error {
	if input == "" {
		return discord.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 Error",
			Description: "Input is required.",
		})
	}

	parsed, err := parsePlayInput(input)
	if err != nil {
		if errors.Is(err, ErrPlayInputTooManyItems) {
			return discord.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
				Title:       "🎵 Error",
				Description: fmt.Sprintf("Too many tracks in one command (max %d).", maxPlayBatchItems),
			})
		}
		return discord.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 Error",
			Description: fmt.Sprintf("Invalid input: %v", err),
		})
	}

	if err := s.InteractionRespond(e.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	}); err != nil {
		return fmt.Errorf("failed to send deferred response: %w", err)
	}

	member := e.Member
	guildID := e.GuildID

	voiceState, err := c.Bot.FindUserVoiceState(guildID, member.User.ID)
	if err != nil {
		discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 Voice Error",
			Description: fmt.Sprintf("%v", err),
		})
		return nil
	}

	ok, err := discord.CheckBotVoicePermissions(s, voiceState.ChannelID)
	if err != nil || !ok {
		discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 Voice Error",
			Description: "I don't have permission to join or speak in that voice channel.",
		})
		return nil
	}

	p := c.Bot.GetOrCreatePlayer(guildID)
	if p == nil {
		discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 Error",
			Description: "Music service is not available.",
		})
		return nil
	}

	switch parsed.Kind {
	case PlayInputKindHistoryIDs:
		if store == nil {
			discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
				Title:       "🎵 Error",
				Description: "Music history storage is not available.",
			})
			return nil
		}
		for _, hid := range parsed.HistoryIDs {
			mp, gerr := store.GetMusicPlayback(guildID, hid)
			if gerr != nil {
				if errors.Is(gerr, storage.ErrMusicPlaybackNotFound) {
					discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
						Title:       "🎵 History",
						Description: "Unknown history id. It may have been removed when the list was trimmed, or the id is wrong.",
					})
				} else {
					discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
						Title:       "🎵 History",
						Description: fmt.Sprintf("Could not load history entry: %v", gerr),
					})
				}
				return nil
			}
			ti := storage.TrackInfoFromMusicPlayback(mp)
			if err := p.EnqueueTrackInfo(ti); err != nil {
				discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
					Title:       "🎵 Queue Error",
					Description: fmt.Sprintf("%v", err),
				})
				return nil
			}
		}

	case PlayInputKindURLs:
		for _, u := range parsed.URLs {
			tracks, resErr := c.Bot.Resolve(guildID, u, src, parser)
			if resErr != nil || len(tracks) == 0 {
				discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
					Title:       "🎵 Error",
					Description: fmt.Sprintf("Failed to resolve track: %v", resErr),
				})
				return nil
			}
			if err := p.EnqueueTrackInfo(tracks[0]); err != nil {
				discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
					Title:       "🎵 Queue Error",
					Description: fmt.Sprintf("%v", err),
				})
				return nil
			}
		}

	case PlayInputKindQuery:
		tracks, resErr := c.Bot.Resolve(guildID, parsed.Query, src, parser)
		if resErr != nil || len(tracks) == 0 {
			discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
				Title:       "🎵 Error",
				Description: fmt.Sprintf("Failed to resolve track: %v", resErr),
			})
			return nil
		}
		if err := p.EnqueueTrackInfo(tracks[0]); err != nil {
			discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
				Title:       "🎵 Queue Error",
				Description: fmt.Sprintf("%v", err),
			})
			return nil
		}
	}

	if !p.IsPlaying() {
		p.PlayNext(voiceState.ChannelID)
	}

	listenPlayerStatusSlash(s, e, p, c.Bot, guildID)
	return nil
}

const historyLinesPerPage = 15

func formatTimelineLine(m domain.MusicPlayback) string {
	title := m.Title
	if title == "" {
		title = "(no title)"
	}
	t := m.PlayedAt.Format("2006-01-02 15:04")
	if m.URL != "" {
		return fmt.Sprintf("`%d` — [%s](%s) — %s", m.ID, title, m.URL, t)
	}
	return fmt.Sprintf("`%d` — %s — %s", m.ID, title, t)
}

func formatCountsLine(r domain.PlaybackCountRow) string {
	title := r.Title
	if title == "" {
		title = "(no title)"
	}
	t := r.LastPlayed.Format("2006-01-02 15:04")
	if r.URL != "" {
		return fmt.Sprintf("`%d` — ×%d — [%s](%s) — last %s", r.RepresentativeID, r.Count, title, r.URL, t)
	}
	return fmt.Sprintf("`%d` — ×%d — %s — last %s", r.RepresentativeID, r.Count, title, t)
}

func (c *MusicCommand) runHistory(s *discordgo.Session, e *discordgo.InteractionCreate, page int64, mode string, store *storage.Storage) error {
	if err := s.InteractionRespond(e.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	}); err != nil {
		return fmt.Errorf("failed to send deferred response: %w", err)
	}

	guildID := e.GuildID
	if c.Bot.GetOrCreatePlayer(guildID) == nil {
		discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 Error",
			Description: "Music service is not available.",
		})
		return nil
	}

	if store == nil {
		discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 Error",
			Description: "Music history storage is not available.",
		})
		return nil
	}

	rows, err := store.ListMusicPlaybackTimeline(guildID)
	if err != nil {
		discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 History",
			Description: fmt.Sprintf("Could not load history: %v", err),
		})
		return nil
	}

	if len(rows) == 0 {
		discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 History",
			Description: "No playback history yet. Use `/music play` first. History is stored per server; very old entries may be removed when the list is trimmed.",
			Color:       discord.EmbedColor,
		})
		return nil
	}

	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "timeline"
	}

	var lines []string
	var totalRows int
	var title string
	var footerExtra string

	switch mode {
	case "counts":
		counts := domain.AggregatePlaybackCounts(rows)
		totalRows = len(counts)
		title = "🎵 Playback history (counts by URL)"
		footerExtra = "Distinct URLs; replay id is the latest play for that link."
		for _, r := range counts {
			lines = append(lines, formatCountsLine(r))
		}
	default:
		totalRows = len(rows)
		title = "🎵 Playback history (timeline)"
		footerExtra = "Chronological; replay with `/music play <id>`."
		for _, m := range rows {
			lines = append(lines, formatTimelineLine(m))
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
		Title:       title,
		Description: desc,
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Page %d/%d (%d rows). %s", page, totalPages, totalRows, footerExtra),
		},
		Color: discord.EmbedColor,
	}
	if err := discord.FollowupEmbed(s, e, embed); err != nil {
		log.Printf("[WARN] FollowupEmbed failed for /music history: %v", err)
	}
	return nil
}

func (c *MusicCommand) runNext(s *discordgo.Session, e *discordgo.InteractionCreate) error {
	guildID := e.GuildID
	member := e.Member

	if err := s.InteractionRespond(e.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	}); err != nil {
		return fmt.Errorf("failed to defer response: %w", err)
	}

	voiceState, err := c.Bot.FindUserVoiceState(guildID, member.User.ID)
	if err != nil {
		discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 Voice Channel Error",
			Description: fmt.Sprintf("Join a voice channel first.\n\n**Error:** %v", err),
		})
		return nil
	}

	ok, err := discord.CheckBotVoicePermissions(s, voiceState.ChannelID)
	if err != nil || !ok {
		discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 Voice Error",
			Description: "I don't have permission to join or speak in that voice channel.",
		})
		return nil
	}

	player := c.Bot.GetOrCreatePlayer(guildID)
	queue := player.Queue()
	if len(queue) == 0 {
		discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 Queue Empty",
			Description: "No tracks left to skip.",
		})
		return nil
	}

	player.Stop(false)
	if err = player.PlayNext(voiceState.ChannelID); err != nil {
		discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 Playback Error",
			Description: fmt.Sprintf("Failed to play next track.\n\n**Error:** %v", err),
		})
		return nil
	}

	listenPlayerStatusSlash(s, e, player, c.Bot, guildID)
	return nil
}

func (c *MusicCommand) runStop(s *discordgo.Session, e *discordgo.InteractionCreate) error {
	guildID := e.GuildID

	if err := s.InteractionRespond(e.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	}); err != nil {
		return fmt.Errorf("failed to defer response: %w", err)
	}

	player := c.Bot.GetOrCreatePlayer(guildID)
	if err := player.Stop(true); err != nil {
		log.Printf("[WARN] Stop error: %v", err)
	}
	stopMsg := "Playback stopped. Queue cleared."
	if err := discord.FollowupEmbed(s, e, &discordgo.MessageEmbed{
		Description: "⏹️ " + stopMsg,
	}); err != nil {
		log.Printf("[WARN] FollowupEmbed failed for /music stop: %v", err)
		_ = discord.EditResponse(s, e, "⏹️ "+stopMsg)
	}
	return nil
}

// statusListenTimeout limits how long we listen for status so the goroutine does not leak.
// Updates after the first use the guild's stored message (edit), so they work beyond token expiry.
const statusListenTimeout = 15 * time.Minute

func listenPlayerStatusSlash(session *discordgo.Session, event *discordgo.InteractionCreate, p *player.Player, bot discord.BotVoice, guildID string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), statusListenTimeout)
		defer cancel()

		for {
			select {
			case <-ctx.Done():
				return
			case signal, ok := <-p.PlayerStatus:
				if !ok {
					return
				}
				switch signal {
				case player.StatusPlaying:
					track := p.CurrentTrack()
					if track == nil {
						_ = bot.UpdateGuildMusicStatus(session, event, guildID, &discordgo.MessageEmbed{
							Title:       "⚠️ Error",
							Description: "Failed to get current track",
						})
						return
					}

					var desc string
					if track.Title != "" && track.URL != "" {
						desc = fmt.Sprintf("🎶 [%s](%s)", track.Title, track.URL)
					} else if track.Title != "" {
						desc = "🎶 " + track.Title
					} else if track.URL != "" {
						desc = "🎶 " + track.URL
					} else {
						desc = "🎶 Unknown track"
					}

					if err := bot.UpdateGuildMusicStatus(session, event, guildID, &discordgo.MessageEmbed{
						Title:       player.StatusPlaying.StringEmoji() + " Now Playing",
						Description: desc,
						Color:       discord.EmbedColor,
					}); err != nil {
						log.Printf("[WARN] UpdateGuildMusicStatus: %v", err)
					}
					return

				case player.StatusAdded:
					if err := bot.UpdateGuildMusicStatus(session, event, guildID, &discordgo.MessageEmbed{
						Title:       player.StatusAdded.StringEmoji() + " Track(s) Added",
						Description: "Added to queue",
						Color:       discord.EmbedColor,
					}); err != nil {
						log.Printf("[WARN] UpdateGuildMusicStatus: %v", err)
					}
					return
				}
			}
		}
	}()
}
