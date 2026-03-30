package music

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/keshon/melodix/internal/discord/command"
	"github.com/keshon/melodix/internal/discord"
	"github.com/keshon/melodix/internal/musicapp"
	"github.com/keshon/melodix/internal/playinput"
	"github.com/keshon/melodix/internal/storage"
	"github.com/keshon/melodix/pkg/music/player"
	"github.com/keshon/melodix/pkg/music/sources"

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
func (c *MusicCommand) Category() string         { return "\U0001f3b5 Music" }
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

func (c *MusicCommand) runPlay(s *discordgo.Session, e *discordgo.InteractionCreate, input, src, parser string, store *storage.Storage) error {
	if input == "" {
		return discord.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       TitleError,
			Description: "Input is required.",
		})
	}

	parsed, err := playinput.ParsePlayInput(input)
	if err != nil {
		if errors.Is(err, playinput.ErrPlayInputTooManyItems) {
			return discord.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
				Title:       TitleError,
				Description: fmt.Sprintf("Too many tracks in one command (max %d).", playinput.MaxPlayBatchItems),
			})
		}
		return discord.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       TitleError,
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
			Title:       TitleVoiceError,
			Description: fmt.Sprintf("%v", err),
		})
		return nil
	}

	ok, err := discord.CheckBotVoicePermissions(s, voiceState.ChannelID)
	if err != nil || !ok {
		discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       TitleVoiceError,
			Description: "I don't have permission to join or speak in that voice channel.",
		})
		return nil
	}

	p := c.Bot.GetOrCreatePlayer(guildID)
	if p == nil {
		discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       TitleError,
			Description: "Music service is not available.",
		})
		return nil
	}

	mu := musicapp.New(store)
	resolve := func(in, source, par string) ([]sources.TrackInfo, error) {
		return c.Bot.Resolve(guildID, in, source, par)
	}
	if err := mu.EnqueueFromParsedInput(p, guildID, parsed, src, parser, resolve, musicapp.QueryViaResolveFirst); err != nil {
		if errors.Is(err, musicapp.ErrStorageUnavailable) {
			discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
				Title:       TitleError,
				Description: "Music history storage is not available.",
			})
			return nil
		}
		if errors.Is(err, storage.ErrMusicPlaybackNotFound) {
			discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
				Title:       TitleHistoryShort,
				Description: "Unknown history id. It may have been removed when the list was trimmed, or the id is wrong.",
			})
			return nil
		}
		if errors.Is(err, player.ErrNoParsersForTrack) {
			discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
				Title:       TitleQueueError,
				Description: fmt.Sprintf("%v", err),
			})
			return nil
		}
		if errors.Is(err, musicapp.ErrNoTracksResolved) {
			discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
				Title:       TitleError,
				Description: fmt.Sprintf("Failed to resolve track: %v", err),
			})
			return nil
		}
		discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       TitleError,
			Description: fmt.Sprintf("Failed to resolve track: %v", err),
		})
		return nil
	}

	if !p.IsPlaying() {
		p.PlayNext(voiceState.ChannelID)
	}

	listenPlayerStatusSlash(s, e, p, c.Bot, guildID)
	return nil
}

func (c *MusicCommand) runHistory(s *discordgo.Session, e *discordgo.InteractionCreate, page int64, view string, store *storage.Storage) error {
	if err := s.InteractionRespond(e.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	}); err != nil {
		return fmt.Errorf("failed to send deferred response: %w", err)
	}

	guildID := e.GuildID
	if c.Bot.GetOrCreatePlayer(guildID) == nil {
		discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       TitleError,
			Description: "Music service is not available.",
		})
		return nil
	}

	if store == nil {
		discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       TitleError,
			Description: "Music history storage is not available.",
		})
		return nil
	}

	mu := musicapp.New(store)
	res, err := mu.BuildHistoryPage(guildID, page, view)
	if errors.Is(err, musicapp.ErrHistoryEmpty) {
		discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       TitleHistoryShort,
			Description: MsgHistoryEmpty,
			Color:       discord.EmbedColor,
		})
		return nil
	}
	if err != nil {
		discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       TitleHistoryShort,
			Description: fmt.Sprintf("Could not load history: %v", err),
		})
		return nil
	}

	embedTitle := TitleHistoryTimeline
	footerExtra := FooterHistoryTimelinePage()
	if res.View == "counts" {
		embedTitle = TitleHistoryCounts
		footerExtra = FooterHistoryCountsPage()
	}

	var b strings.Builder
	if res.View == "counts" {
		for _, r := range res.Counts {
			b.WriteString(FormatCountsLine(r))
			b.WriteByte('\n')
		}
	} else {
		for _, m := range res.Rows {
			b.WriteString(FormatTimelineLine(m))
			b.WriteByte('\n')
		}
	}
	desc := strings.TrimSpace(b.String())
	if len(desc) > 4000 {
		desc = desc[:3997] + "..."
	}

	embed := &discordgo.MessageEmbed{
		Title:       embedTitle,
		Description: desc,
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Page %d/%d (%d rows). %s", res.Page, res.TotalPages, res.TotalRows, footerExtra),
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
			Title:       TitleVoiceChannelError,
			Description: fmt.Sprintf("Join a voice channel first.\n\n**Error:** %v", err),
		})
		return nil
	}

	ok, err := discord.CheckBotVoicePermissions(s, voiceState.ChannelID)
	if err != nil || !ok {
		discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       TitleVoiceError,
			Description: "I don't have permission to join or speak in that voice channel.",
		})
		return nil
	}

	player := c.Bot.GetOrCreatePlayer(guildID)
	queue := player.Queue()
	if len(queue) == 0 {
		discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       TitleQueueEmpty,
			Description: "No tracks left to skip.",
		})
		return nil
	}

	player.Stop(false)
	if err = player.PlayNext(voiceState.ChannelID); err != nil {
		discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       TitlePlaybackError,
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
		Description: PrefixStop + stopMsg,
	}); err != nil {
		log.Printf("[WARN] FollowupEmbed failed for /music stop: %v", err)
		_ = discord.EditResponse(s, e, PrefixStop+stopMsg)
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
							Title:       TitleWarnError,
							Description: "Failed to get current track",
						})
						return
					}

					label := track.DisplayLabel()
					desc := NowPlayingMarkdown(label, track.URL)

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
