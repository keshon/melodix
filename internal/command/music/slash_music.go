package music

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	appmusic "github.com/keshon/melodix/internal/app/music"
	"github.com/keshon/melodix/internal/command"
	"github.com/keshon/melodix/internal/discord"
	"github.com/keshon/melodix/internal/domain"
	"github.com/keshon/melodix/internal/storage"
	"github.com/keshon/melodix/pkg/music/player"

	"github.com/bwmarrin/discordgo"
)

type MusicCommand struct {
	Bot discord.DiscordMusicBot
	App *appmusic.Service
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
			},
		},
	}
}

func (c *MusicCommand) Run(ctx interface{}) error {
	slashCtx, ok := ctx.(*command.SlashInteractionContext)
	if !ok {
		return nil
	}
	return c.RunSlash(slashCtx)
}

// RunSlash implements command.SlashRunner for typed slash handling.
func (c *MusicCommand) RunSlash(slashCtx *command.SlashInteractionContext) error {
	s := slashCtx.Session
	e := slashCtx.Event
	store := slashCtx.Storage
	reqCtx := slashCtx.Ctx
	if reqCtx == nil {
		reqCtx = context.Background()
	}

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
		return c.runPlay(reqCtx, s, e, input, source, parser, store)

	case "next":
		return c.runNext(s, e)

	case "stop":
		return c.runStop(s, e)

	case "history":
		view := "timeline"
		var page int64 = 1
		for _, opt := range sub.Options {
			switch opt.Name {
			case "view":
				if v := strings.TrimSpace(opt.StringValue()); v != "" {
					view = v
				}
			case "page":
				page = opt.IntValue()
			}
		}
		return c.runHistory(s, e, page, view, store)

	default:
		return discord.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Description: fmt.Sprintf("Unknown subcommand: %s", sub.Name),
		})
	}
}

func (c *MusicCommand) runPlay(ctx context.Context, s *discordgo.Session, e *discordgo.InteractionCreate, input, src, parser string, store *storage.Storage) error {
	if input == "" {
		return discord.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 Error",
			Description: "Input is required.",
		})
	}

	parsed, err := appmusic.ParsePlayInput(input)
	if err != nil {
		if errors.Is(err, appmusic.ErrPlayInputTooManyItems) {
			return discord.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
				Title:       "🎵 Error",
				Description: fmt.Sprintf("Too many tracks in one command (max %d).", appmusic.MaxPlayBatchItems),
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

	var repo domain.MusicHistoryRepository
	if store != nil {
		repo = store
	}

	playErr := c.App.Play(ctx, guildID, voiceState.ChannelID, parsed, src, parser, repo)
	if playErr != nil {
		switch {
		case errors.Is(playErr, appmusic.ErrMusicUnavailable):
			discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
				Title:       "🎵 Error",
				Description: "Music service is not available.",
			})
		case errors.Is(playErr, appmusic.ErrHistoryUnavailable):
			discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
				Title:       "🎵 Error",
				Description: "Music history storage is not available.",
			})
		case errors.Is(playErr, appmusic.ErrPlayNoTracksResolved):
			discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
				Title:       "🎵 Nothing found",
				Description: "No tracks were found for that input. Try a different query or a direct link.",
			})
		case errors.Is(playErr, appmusic.ErrPlayResolveFailed):
			discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
				Title:       "🎵 Resolve Error",
				Description: "Failed to resolve that input. Try again, or try a direct link.",
			})
		case errors.Is(playErr, appmusic.ErrPlayEnqueueTrackFailed):
			// Keep a generic message; details are in logs via wrapped error.
			discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
				Title:       "🎵 Queue Error",
				Description: "Failed to add track to the queue.",
			})
		case errors.Is(playErr, domain.ErrMusicPlaybackNotFound):
			discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
				Title:       "🎵 History",
				Description: "Unknown history id. It may have been removed when the list was trimmed, or the id is wrong.",
			})
		case errors.Is(playErr, player.ErrNoParsersForTrack):
			discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
				Title:       "🎵 Unsupported Track",
				Description: "That track can't be played (no supported parsers). Try a different link or source.",
			})
		default:
			discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
				Title:       "🎵 Error",
				Description: fmt.Sprintf("Failed to play: %v", playErr),
			})
		}
		return nil
	}

	p := c.Bot.GetOrCreatePlayer(guildID)
	if p != nil {
		listenPlayerStatusSlash(s, e, p, c.Bot, guildID)
	}
	return nil
}

func (c *MusicCommand) runHistory(s *discordgo.Session, e *discordgo.InteractionCreate, page int64, view string, store *storage.Storage) error {
	if err := s.InteractionRespond(e.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	}); err != nil {
		return fmt.Errorf("failed to send deferred response: %w", err)
	}

	guildID := e.GuildID

	var repo domain.MusicHistoryRepository
	if store != nil {
		repo = store
	}

	res, err := c.App.HistoryPage(guildID, page, view, repo)
	if err != nil {
		if errors.Is(err, appmusic.ErrMusicUnavailable) {
			discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
				Title:       "🎵 Error",
				Description: "Music service is not available.",
			})
			return nil
		}
		if errors.Is(err, appmusic.ErrHistoryUnavailable) {
			discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
				Title:       "🎵 Error",
				Description: "Music history storage is not available.",
			})
			return nil
		}
		discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 History",
			Description: fmt.Sprintf("Could not load history: %v", err),
		})
		return nil
	}

	if res.TotalRows == 0 {
		discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 History",
			Description: "No playback history yet. Use `/music play` first. History is stored per server; very old entries may be removed when the list is trimmed.",
			Color:       discord.EmbedColor,
		})
		return nil
	}

	lines := FormatHistoryLines(res)
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	desc := strings.TrimSpace(b.String())
	if len(desc) > 4000 {
		desc = desc[:3997] + "..."
	}

	embed := &discordgo.MessageEmbed{
		Title:       historyEmbedTitle(res.View),
		Description: desc,
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Page %d/%d (%d rows). %s", res.Page, res.TotalPages, res.TotalRows, historyFooterExtra(res.View)),
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

	skipErr := c.App.Skip(guildID, voiceState.ChannelID)
	if skipErr != nil {
		switch {
		case errors.Is(skipErr, appmusic.ErrMusicUnavailable):
			discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
				Title:       "🎵 Error",
				Description: "Music service is not available.",
			})
		case errors.Is(skipErr, appmusic.ErrQueueEmpty):
			discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
				Title:       "🎵 Queue Empty",
				Description: "No tracks left to skip.",
			})
		default:
			discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
				Title:       "🎵 Playback Error",
				Description: fmt.Sprintf("Failed to play next track.\n\n**Error:** %v", skipErr),
			})
		}
		return nil
	}

	player := c.Bot.GetOrCreatePlayer(guildID)
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

	if err := c.App.Stop(guildID); err != nil {
		if errors.Is(err, appmusic.ErrMusicUnavailable) {
			discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
				Title:       "🎵 Error",
				Description: "Music service is not available.",
			})
			return nil
		}
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

func listenPlayerStatusSlash(session *discordgo.Session, event *discordgo.InteractionCreate, p *player.Player, bot discord.MusicPresenter, guildID string) {
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
