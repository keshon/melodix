package music

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/keshon/melodix/internal/command"
	"github.com/keshon/melodix/internal/discord"
	"github.com/keshon/melodix/internal/music/player"

	"github.com/bwmarrin/discordgo"
)

type MusicCommand struct {
	Bot discord.BotVoice
}

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
		},
	}
}

func (c *MusicCommand) Run(ctx interface{}) error {
	context, ok := ctx.(*command.SlashInteractionContext)
	if !ok {
		return nil
	}

	s := context.Session
	e := context.Event

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
		return c.runPlay(s, e, input, source, parser)

	case "next":
		return c.runNext(s, e)

	case "stop":
		return c.runStop(s, e)

	default:
		return discord.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Description: fmt.Sprintf("Unknown subcommand: %s", sub.Name),
		})
	}
}

func (c *MusicCommand) runPlay(s *discordgo.Session, e *discordgo.InteractionCreate, input, src, parser string) error {
	if input == "" {
		return discord.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 Error",
			Description: "Input is required.",
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

	tracks, err := c.Bot.Resolve(guildID, input, src, parser)
	if err != nil || len(tracks) == 0 {
		discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 Error",
			Description: fmt.Sprintf("Failed to resolve track: %v", err),
		})
		return nil
	}

	player := c.Bot.GetOrCreatePlayer(guildID)
	err = player.EnqueueTrackInfo(tracks[0])
	if err != nil {
		discord.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 Queue Error",
			Description: fmt.Sprintf("%v", err),
		})
		return nil
	}

	if !player.IsPlaying() {
		player.PlayNext(voiceState.ChannelID)
	}

	listenPlayerStatusSlash(s, e, player, c.Bot, guildID)
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
	discord.FollowupEmbed(s, e, &discordgo.MessageEmbed{
		Description: "⏹️ Playback stopped. Queue cleared.",
	})
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
