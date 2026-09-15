package play

import (
	"errors"
	"fmt"

	"github.com/keshon/melodix/internal/command/music/common"
	"github.com/keshon/melodix/internal/command/music/playback"
	"github.com/keshon/melodix/internal/discord"
	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/storage"
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

func (c *Play) SlashDefinition() *cmdadapter.SlashCommand {
	return &cmdadapter.SlashCommand{
		Name:        c.Name(),
		Description: c.Description(),
		Options: []cmdadapter.SlashOption{
			{
				Type:        cmdadapter.OptionString,
				Name:        "input",
				Description: "Link, search query, or history id(s)",
				Required:    true,
			},
			{
				Type:        cmdadapter.OptionString,
				Name:        "source",
				Description: "Specify a source if search query is used",
				Choices: []cmdadapter.SlashChoice{
					{Name: "YouTube", Value: sources.YouTube},
					{Name: "SoundCloud", Value: sources.SoundCloud},
					{Name: "Radio", Value: sources.Radio},
				},
			},
			{
				Type:        cmdadapter.OptionString,
				Name:        "parser",
				Description: "Override autodetect parser",
				Choices: []cmdadapter.SlashChoice{
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

func (c *Play) Run(slashCtx *cmdadapter.SlashInteractionContext) error {

	store := slashCtx.Storage

	input := slashCtx.StringOption("input")
	source := slashCtx.StringOption("source")
	parser := slashCtx.StringOption("parser")

	if input == "" {
		return slashCtx.RespondEphemeral(&cmdadapter.Embed{
			Title:       "🎵 Error",
			Description: "Input is required.",
		})
	}

	parsed, err := common.ParsePlayInput(input)
	if err != nil {
		if errors.Is(err, common.ErrPlayInputTooManyItems) {
			return slashCtx.RespondEphemeral(&cmdadapter.Embed{
				Title:       "🎵 Error",
				Description: "Too many tracks in one command.",
			})
		}
		return slashCtx.RespondEphemeral(&cmdadapter.Embed{
			Title:       "🎵 Error",
			Description: fmt.Sprintf("Invalid input: %v", err),
		})
	}

	if err := slashCtx.Defer(); err != nil {
		return fmt.Errorf("failed to send deferred response: %w", err)
	}

	guildID := slashCtx.GuildID()
	target, ok := playback.Join(c.Bot, slashCtx)
	if !ok {
		return nil
	}
	p := target.Player
	added := 0

	switch parsed.Kind {
	case common.PlayInputKindHistoryIDs:
		if store == nil {
			slashCtx.FollowupEphemeral(&cmdadapter.Embed{
				Title:       "🎵 Error",
				Description: "Music history storage is not available.",
			})
			return nil
		}
		// Collected first, enqueued once: a batch emits a single queue update.
		batch := make([]sources.TrackInfo, 0, len(parsed.HistoryIDs))
		for _, hid := range parsed.HistoryIDs {
			mp, gerr := store.MusicPlayback(guildID, hid)
			if gerr != nil {
				if errors.Is(gerr, storage.ErrMusicPlaybackNotFound) {
					slashCtx.FollowupEphemeral(&cmdadapter.Embed{
						Title:       "🎵 History",
						Description: "Unknown history id. It may have been removed when the list was trimmed, or the id is wrong.",
					})
				} else {
					slashCtx.FollowupEphemeral(&cmdadapter.Embed{
						Title:       "🎵 History",
						Description: fmt.Sprintf("Could not load history entry: %v", gerr),
					})
				}
				return nil
			}
			batch = append(batch, storage.TrackInfoFromMusicPlayback(mp))
		}
		if err := p.EnqueueTrackInfos(batch); err != nil {
			playback.QueueError(slashCtx, err)
			return nil
		}
		added = len(batch)

	case common.PlayInputKindURLs:
		batch := make([]sources.TrackInfo, 0, len(parsed.URLs))
		for _, u := range parsed.URLs {
			tracks, resErr := c.Bot.ResolveTracks(guildID, u, source, parser)
			if resErr != nil || len(tracks) == 0 {
				slashCtx.FollowupEphemeral(&cmdadapter.Embed{
					Title:       "🎵 Error",
					Description: fmt.Sprintf("Failed to resolve track: %v", resErr),
				})
				return nil
			}
			batch = append(batch, tracks...)
		}
		if err := p.EnqueueTrackInfos(batch); err != nil {
			playback.QueueError(slashCtx, err)
			return nil
		}
		added = len(batch)

	case common.PlayInputKindQuery:
		tracks, resErr := c.Bot.ResolveTracks(guildID, parsed.Query, source, parser)
		if resErr != nil || len(tracks) == 0 {
			slashCtx.FollowupEphemeral(&cmdadapter.Embed{
				Title:       "🎵 Error",
				Description: fmt.Sprintf("Failed to resolve track: %v", resErr),
			})
			return nil
		}
		if err := p.EnqueueTrackInfos(tracks); err != nil {
			playback.QueueError(slashCtx, err)
			return nil
		}
		added = len(tracks)
	}

	playback.StartAndRender(c.Bot, slashCtx, slashCtx.AppLog, target, added)
	return nil
}
