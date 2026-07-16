package play

import (
	"errors"
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/internal/command/music/common"
	"github.com/keshon/melodix/internal/discord"
	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/discord/perm"
	"github.com/keshon/melodix/internal/discord/reply"
	"github.com/keshon/melodix/internal/storage"
	"github.com/keshon/melodix/pkg/music/player"
	"github.com/keshon/melodix/pkg/music/sources"
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
					{Name: "YouTube", Value: sources.YouTube},
					{Name: "SoundCloud", Value: sources.SoundCloud},
					{Name: "Radio", Value: sources.Radio},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "parser",
				Description: "Override autodetect parser",
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "youtube native", Value: sources.ParserYtnativeLink},
					{Name: "soundcloud native", Value: sources.ParserScnativeLink},
					{Name: "ytdlp pipe", Value: sources.ParserYtdlpPipe},
					{Name: "ytdlp link", Value: sources.ParserYtdlpLink},
					{Name: "kkdai pipe", Value: sources.ParserKkdaiPipe},
					{Name: "kkdai link", Value: sources.ParserKkdaiLink},
					{Name: "ffmpeg direct link", Value: sources.ParserFFmpegLink},
				},
			},
		},
	}
}

func (c *Play) Run(ctx interface{}) error {
	slashCtx, ok := ctx.(*cmdadapter.SlashInteractionContext)
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
		return reply.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 Error",
			Description: "Input is required.",
		})
	}

	parsed, err := common.ParsePlayInput(input)
	if err != nil {
		if errors.Is(err, common.ErrPlayInputTooManyItems) {
			return reply.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
				Title:       "🎵 Error",
				Description: "Too many tracks in one command.",
			})
		}
		return reply.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
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
		reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 Voice Error",
			Description: fmt.Sprintf("%v", err),
		})
		return nil
	}

	permOK, err := perm.CheckBotVoicePermissions(s, voiceState.ChannelID)
	if err != nil || !permOK {
		reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 Voice Error",
			Description: "I don't have permission to join or speak in that voice channel.",
		})
		return nil
	}

	c.Bot.SetGuildMusicNotifyChannel(guildID, e.ChannelID)

	p := c.Bot.GetOrCreatePlayer(guildID)
	if p == nil {
		reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 Error",
			Description: "Music service is not available.",
		})
		return nil
	}

	switch parsed.Kind {
	case common.PlayInputKindHistoryIDs:
		if store == nil {
			reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
				Title:       "🎵 Error",
				Description: "Music history storage is not available.",
			})
			return nil
		}
		for _, hid := range parsed.HistoryIDs {
			mp, gerr := store.MusicPlayback(guildID, hid)
			if gerr != nil {
				if errors.Is(gerr, storage.ErrMusicPlaybackNotFound) {
					reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
						Title:       "🎵 History",
						Description: "Unknown history id. It may have been removed when the list was trimmed, or the id is wrong.",
					})
				} else {
					reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
						Title:       "🎵 History",
						Description: fmt.Sprintf("Could not load history entry: %v", gerr),
					})
				}
				return nil
			}
			ti := storage.TrackInfoFromMusicPlayback(mp)
			if err := p.EnqueueTrackInfo(ti); err != nil {
				reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
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
				reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
					Title:       "🎵 Error",
					Description: fmt.Sprintf("Failed to resolve track: %v", resErr),
				})
				return nil
			}
			if err := p.EnqueueTrackInfo(tracks[0]); err != nil {
				reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
					Title:       "🎵 Queue Error",
					Description: fmt.Sprintf("%v", err),
				})
				return nil
			}
		}

	case common.PlayInputKindQuery:
		tracks, resErr := c.Bot.ResolveTracks(guildID, parsed.Query, source, parser)
		if resErr != nil || len(tracks) == 0 {
			reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
				Title:       "🎵 Error",
				Description: fmt.Sprintf("Failed to resolve track: %v", resErr),
			})
			return nil
		}
		if err := p.EnqueueTrackInfo(tracks[0]); err != nil {
			reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
				Title:       "🎵 Queue Error",
				Description: fmt.Sprintf("%v", err),
			})
			return nil
		}
	}

	started := false
	if !p.IsPlaying() {
		if err := p.PlayNext(voiceState.ChannelID); err != nil {
			if errors.Is(err, player.ErrTrackStartFailed) {
				reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
					Title:       "🎵 Playback Error",
					Description: common.PlaybackErrorDescription(err),
					Color:       reply.EmbedColor,
				})
				return nil
			}
			if errors.Is(err, player.ErrNoTracksInQueue) {
				reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
					Title:       "🎵 Queue",
					Description: "Nothing is in the queue to play.",
					Color:       reply.EmbedColor,
				})
				return nil
			}
			reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
				Title:       "🎵 Playback Error",
				Description: fmt.Sprintf("%v", err),
				Color:       reply.EmbedColor,
			})
			return nil
		}
		started = true
	}

	// The outcome is known here, so render it synchronously (async transitions such as
	// auto-advance are handled by the voice service's status watcher).
	embed := reply.TracksAddedEmbed()
	if started {
		if track := p.CurrentTrack(); track != nil {
			embed = reply.NowPlayingEmbed(track)
		}
	}
	if err := c.Bot.UpdatePlaybackStatus(s, e, guildID, embed); err != nil {
		slashCtx.AppLog.Warn().Str("guild_id", guildID).Err(err).Msg("guild_status_update_failed")
	}
	return nil
}
