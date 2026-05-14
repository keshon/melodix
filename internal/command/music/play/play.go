package play

import (
	"errors"
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/internal/command"
	"github.com/keshon/melodix/internal/command/music/common"
	"github.com/keshon/melodix/internal/discord"
	"github.com/keshon/melodix/internal/discord/discordreply"
	"github.com/keshon/melodix/internal/discord/perm"
	"github.com/keshon/melodix/internal/storage"
	"github.com/keshon/melodix/pkg/music/player"
)

type Play struct {
	Bot discord.VoiceAPI
}

func (c *Play) Name() string             { return "play" }
func (c *Play) Description() string      { return "Play a music track" }
func (c *Play) Group() string            { return "music" }
func (c *Play) Category() string         { return "🎵 Music" }
func (c *Play) UserPermissions() []int64 { return []int64{} }

func (c *Play) SlashDefinition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        c.Name(),
		Description: c.Description(),
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "input",
				Description: "Link, search query, or history id(s)",
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
	}
}

func (c *Play) Run(ctx interface{}) error {
	slashCtx, ok := ctx.(*command.SlashInteractionContext)
	if !ok {
		return nil
	}

	s := slashCtx.Session
	e := slashCtx.Event
	store := slashCtx.Storage

	var input, source, parser string
	for _, opt := range e.ApplicationCommandData().Options {
		switch opt.Name {
		case "input":
			input = opt.StringValue()
		case "source":
			source = opt.StringValue()
		case "parser":
			parser = opt.StringValue()
		}
	}

	if input == "" {
		return discordreply.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 Error",
			Description: "Input is required.",
		})
	}

	parsed, err := common.ParsePlayInput(input)
	if err != nil {
		if errors.Is(err, common.ErrPlayInputTooManyItems) {
			return discordreply.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
				Title:       "🎵 Error",
				Description: "Too many tracks in one command.",
			})
		}
		return discordreply.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
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
		discordreply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 Voice Error",
			Description: fmt.Sprintf("%v", err),
		})
		return nil
	}

	permOK, err := perm.CheckBotVoicePermissions(s, voiceState.ChannelID)
	if err != nil || !permOK {
		discordreply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 Voice Error",
			Description: "I don't have permission to join or speak in that voice channel.",
		})
		return nil
	}

	c.Bot.SetGuildMusicNotifyChannel(guildID, e.ChannelID)

	p := c.Bot.GetOrCreatePlayer(guildID)
	if p == nil {
		discordreply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 Error",
			Description: "Music service is not available.",
		})
		return nil
	}

	switch parsed.Kind {
	case common.PlayInputKindHistoryIDs:
		if store == nil {
			discordreply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
				Title:       "🎵 Error",
				Description: "Music history storage is not available.",
			})
			return nil
		}
		for _, hid := range parsed.HistoryIDs {
			mp, gerr := store.MusicPlayback(guildID, hid)
			if gerr != nil {
				if errors.Is(gerr, storage.ErrMusicPlaybackNotFound) {
					discordreply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
						Title:       "🎵 History",
						Description: "Unknown history id. It may have been removed when the list was trimmed, or the id is wrong.",
					})
				} else {
					discordreply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
						Title:       "🎵 History",
						Description: fmt.Sprintf("Could not load history entry: %v", gerr),
					})
				}
				return nil
			}
			ti := storage.TrackInfoFromMusicPlayback(mp)
			if err := p.EnqueueTrackInfo(ti); err != nil {
				discordreply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
					Title:       "🎵 Queue Error",
					Description: fmt.Sprintf("%v", err),
				})
				return nil
			}
		}

	case common.PlayInputKindURLs:
		for _, u := range parsed.URLs {
			tracks, resErr := c.Bot.ResolveTracks(guildID, u, source, parser)
			if resErr != nil || len(tracks) == 0 {
				discordreply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
					Title:       "🎵 Error",
					Description: fmt.Sprintf("Failed to resolve track: %v", resErr),
				})
				return nil
			}
			if err := p.EnqueueTrackInfo(tracks[0]); err != nil {
				discordreply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
					Title:       "🎵 Queue Error",
					Description: fmt.Sprintf("%v", err),
				})
				return nil
			}
		}

	case common.PlayInputKindQuery:
		tracks, resErr := c.Bot.ResolveTracks(guildID, parsed.Query, source, parser)
		if resErr != nil || len(tracks) == 0 {
			discordreply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
				Title:       "🎵 Error",
				Description: fmt.Sprintf("Failed to resolve track: %v", resErr),
			})
			return nil
		}
		if err := p.EnqueueTrackInfo(tracks[0]); err != nil {
			discordreply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
				Title:       "🎵 Queue Error",
				Description: fmt.Sprintf("%v", err),
			})
			return nil
		}
	}

	if !p.IsPlaying() {
		if err := p.PlayNext(voiceState.ChannelID); err != nil {
			if errors.Is(err, player.ErrTrackStartFailed) {
				discordreply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
					Title:       "🎵 Playback Error",
					Description: common.PlaybackErrorDescription(err),
					Color:       discordreply.EmbedColor,
				})
				return nil
			}
			if errors.Is(err, player.ErrNoTracksInQueue) {
				discordreply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
					Title:       "🎵 Queue",
					Description: "Nothing is in the queue to play.",
					Color:       discordreply.EmbedColor,
				})
				return nil
			}
			discordreply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
				Title:       "🎵 Playback Error",
				Description: fmt.Sprintf("%v", err),
				Color:       discordreply.EmbedColor,
			})
			return nil
		}
	}

	common.ListenPlayerStatusSlash(s, e, p, c.Bot, guildID, slashCtx.AppLog)
	return nil
}
