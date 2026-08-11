package play

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"

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

var youtubeURLRegex = regexp.MustCompile(`https?://(?:www\.)?(?:youtube\.com|youtu\.be)/[^\s<>\]]+`)

func normalizeInputURL(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}
	// Remove common escaping from Markdown/Discord
	input = strings.ReplaceAll(input, `\&`, "&")
	input = strings.ReplaceAll(input, `\_`, "_")
	input = strings.ReplaceAll(input, `\?`, "?")
	input = strings.ReplaceAll(input, `\=`, "=")
	input = strings.ReplaceAll(input, `\#`, "#")
	if match := youtubeURLRegex.FindString(input); match != "" {
		input = match
	}
	input = strings.TrimSpace(input)
	input = strings.TrimSuffix(input, `\`)
	input = strings.TrimSuffix(input, ")")
	input = strings.TrimSuffix(input, "]")
	return input
}

func canonicalYouTubeURL(input string) string {
	input = normalizeInputURL(input)
	if input == "" {
		return ""
	}
	parsedURL, err := url.Parse(input)
	if err != nil {
		return input
	}
	listID := parsedURL.Query().Get("list")
	if listID == "" {
		return input
	}
	return "https://www.youtube.com/playlist?list=" + url.QueryEscape(listID)
}

func isYouTubeURL(input string) bool {
	input = normalizeInputURL(input)
	return strings.Contains(input, "youtube.com") || strings.Contains(input, "youtu.be")
}

func isYouTubePlaylistURL(input string) bool {
	input = normalizeInputURL(input)
	if !isYouTubeURL(input) {
		return false
	}
	parsedURL, err := url.Parse(input)
	if err != nil {
		return false
	}
	return parsedURL.Query().Get("list") != ""
}

// findYTDLP returns the path to yt-dlp executable.
func findYTDLP() (string, error) {
	path, err := exec.LookPath("yt-dlp")
	if err != nil {
		return "", fmt.Errorf("yt-dlp not found in PATH: %w", err)
	}
	return path, nil
}

// buildYTDLPArgs constructs the command line for yt-dlp.
// It optionally uses cookies from file or browser via environment variables:
//   - YTDLP_COOKIES  : path to cookies.txt
//   - YTDLP_BROWSER  : browser name (firefox, chrome, etc.)
func buildYTDLPArgs(playlistURL string) []string {
	args := []string{
		"--flat-playlist",
		"--skip-download",
		"--no-warnings",
		"--print", "%(id)s",
	}
	if browser := strings.TrimSpace(os.Getenv("YTDLP_BROWSER")); browser != "" {
		args = append([]string{"--cookies-from-browser", browser}, args...)
	}
	if cookies := strings.TrimSpace(os.Getenv("YTDLP_COOKIES")); cookies != "" {
		args = append([]string{"--cookies", cookies}, args...)
	}
	args = append(args, playlistURL)
	return args
}

// extractYouTubePlaylist returns a list of video IDs from a YouTube playlist.
func extractYouTubePlaylist(input string) ([]string, error) {
	playlistURL := canonicalYouTubeURL(input)
	if playlistURL == "" {
		return nil, errors.New("empty YouTube playlist URL")
	}
	ytDLP, err := findYTDLP()
	if err != nil {
		return nil, err
	}
	args := buildYTDLPArgs(playlistURL)
	cmd := exec.Command(ytDLP, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	stdoutText := strings.TrimSpace(stdout.String())
	stderrText := strings.TrimSpace(stderr.String())
	if err != nil {
		if stderrText == "" {
			return nil, fmt.Errorf("yt-dlp failed: %w", err)
		}
		return nil, fmt.Errorf("yt-dlp failed: %w: %s", err, stderrText)
	}
	if stdoutText == "" {
		return nil, fmt.Errorf("yt-dlp returned no playlist entries; stderr=%s", stderrText)
	}
	lines := strings.Split(stdoutText, "\n")
	var ids []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Lines may contain warnings or extra text, but we only care about a single video ID (11 chars)
		// We also allow for a tab if we used "%(id)s" only, it's just the ID.
		// For safety, we extract the first non-empty token that looks like a YouTube ID.
		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}
		candidate := parts[0]
		// Check if it looks like a YouTube video ID (11 chars, alphanumeric and _-)
		if len(candidate) >= 10 && len(candidate) <= 12 && regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(candidate) {
			ids = append(ids, candidate)
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no usable video IDs; stdout=%s; stderr=%s", stdoutText, stderrText)
	}
	return ids, nil
}

// -----------------------------------------------------------------------------
// Play command
// -----------------------------------------------------------------------------

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

	input = normalizeInputURL(input)

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
	parsed.Query = normalizeInputURL(parsed.Query)
	for i := range parsed.URLs {
		parsed.URLs[i] = normalizeInputURL(parsed.URLs[i])
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

	totalAdded := 0

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
			totalAdded++
		}

	case common.PlayInputKindURLs:
		for _, rawURL := range parsed.URLs {
			u := normalizeInputURL(rawURL)
			if isYouTubePlaylistURL(u) {
				videoIDs, err := extractYouTubePlaylist(u)
				if err != nil {
					reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
						Title:       "🎵 Playlist Error",
						Description: fmt.Sprintf("Could not extract the YouTube playlist.\n\n`%v`", err),
					})
					return nil
				}
				for _, id := range videoIDs {
					videoURL := "https://www.youtube.com/watch?v=" + id
					tracks, resErr := c.Bot.ResolveTracks(guildID, videoURL, source, parser)
					if resErr != nil || len(tracks) == 0 {
						slashCtx.AppLog.Warn().
							Str("video_id", id).
							Err(resErr).
							Msg("Failed to resolve single video from playlist, skipping")
						continue
					}
					for _, track := range tracks {
						if err := p.EnqueueTrackInfo(track); err != nil {
							reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
								Title:       "🎵 Queue Error",
								Description: fmt.Sprintf("%v", err),
							})
							return nil
						}
						totalAdded++
					}
				}
				continue
			}
			// Normal single URL
			tracks, resErr := c.Bot.ResolveTracks(guildID, u, source, parser)
			if resErr != nil || len(tracks) == 0 {
				reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
					Title:       "🎵 Error",
					Description: fmt.Sprintf("Failed to resolve track: %v", resErr),
				})
				return nil
			}
			for _, track := range tracks {
				if err := p.EnqueueTrackInfo(track); err != nil {
					reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
						Title:       "🎵 Queue Error",
						Description: fmt.Sprintf("%v", err),
					})
					return nil
				}
				totalAdded++
			}
		}

	case common.PlayInputKindQuery:
		query := normalizeInputURL(parsed.Query)
		if isYouTubePlaylistURL(query) {
			videoIDs, err := extractYouTubePlaylist(query)
			if err != nil {
				reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
					Title:       "🎵 Playlist Error",
					Description: fmt.Sprintf("Could not extract the YouTube playlist.\n\n`%v`", err),
				})
				return nil
			}
			for _, id := range videoIDs {
				videoURL := "https://www.youtube.com/watch?v=" + id
				tracks, resErr := c.Bot.ResolveTracks(guildID, videoURL, source, parser)
				if resErr != nil || len(tracks) == 0 {
					slashCtx.AppLog.Warn().
						Str("video_id", id).
						Err(resErr).
						Msg("Failed to resolve single video from playlist, skipping")
					continue
				}
				for _, track := range tracks {
					if err := p.EnqueueTrackInfo(track); err != nil {
						reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
							Title:       "🎵 Queue Error",
							Description: fmt.Sprintf("%v", err),
						})
						return nil
					}
					totalAdded++
				}
			}
		} else {
			// Normal search query
			tracks, resErr := c.Bot.ResolveTracks(guildID, query, source, parser)
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
			totalAdded++
		}
	}

	if totalAdded == 0 {
		reply.FollowupEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Title:       "🎵 Queue",
			Description: "No tracks were added.",
		})
		return nil
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

	var embed *discordgo.MessageEmbed
	if started {
		if track := p.CurrentTrack(); track != nil {
			embed = reply.NowPlayingEmbed(track)
			if totalAdded > 1 {
				embed.Description = fmt.Sprintf("Added **%d** tracks to the queue.", totalAdded)
			}
		} else {
			embed = reply.TracksAddedEmbed()
		}
	} else {
		embed = reply.TracksAddedEmbed()
		if totalAdded > 1 {
			embed.Description = fmt.Sprintf("Added **%d** tracks to the queue.", totalAdded)
		}
	}

	if err := c.Bot.UpdatePlaybackStatus(s, e, guildID, embed); err != nil {
		slashCtx.AppLog.Warn().Str("guild_id", guildID).Err(err).Msg("guild_status_update_failed")
	}
	return nil
}