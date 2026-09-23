package play

import (
	"errors"
	"fmt"

	"github.com/keshon/melodix/internal/discord"
	"github.com/keshon/melodix/internal/discord/adapter"
	"github.com/keshon/melodix/internal/discord/command/music/playback"
	"github.com/keshon/melodix/internal/music"
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

func (c *Play) SlashDefinition() *adapter.SlashCommand {
	return &adapter.SlashCommand{
		Name:        c.Name(),
		Description: c.Description(),
		Options: []adapter.SlashOption{
			{
				Type:        adapter.OptionString,
				Name:        "input",
				Description: "Link, search query, or history id(s)",
				Required:    true,
			},
			{
				Type:        adapter.OptionString,
				Name:        "source",
				Description: "Specify a source if search query is used",
				Choices: []adapter.SlashChoice{
					{Name: "YouTube", Value: sources.YouTube},
					{Name: "SoundCloud", Value: sources.SoundCloud},
					{Name: "Radio", Value: sources.Radio},
				},
			},
			{
				Type:        adapter.OptionString,
				Name:        "parser",
				Description: "Override autodetect parser",
				Choices: []adapter.SlashChoice{
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

func (c *Play) Run(slashCtx *adapter.SlashInteractionContext) error {
	input := slashCtx.StringOption("input")
	source := slashCtx.StringOption("source")
	parser := slashCtx.StringOption("parser")

	if input == "" {
		return slashCtx.RespondEphemeral(&adapter.Embed{
			Title:       "🎵 Error",
			Description: "Input is required.",
		})
	}

	in, err := music.ParseInput(input)
	if err != nil {
		if errors.Is(err, music.ErrTooManyItems) {
			return slashCtx.RespondEphemeral(&adapter.Embed{
				Title:       "🎵 Error",
				Description: "Too many tracks in one command.",
			})
		}
		return slashCtx.RespondEphemeral(&adapter.Embed{
			Title:       "🎵 Error",
			Description: fmt.Sprintf("Invalid input: %v", err),
		})
	}

	if err := slashCtx.Defer(); err != nil {
		return fmt.Errorf("failed to send deferred response: %w", err)
	}

	target, ok := playback.Join(c.Bot, slashCtx)
	if !ok {
		return nil
	}
	added, err := c.Bot.Music().Add(target.GuildID, in, source, parser)
	if err != nil {
		playback.AddError(slashCtx, err)
		return nil
	}

	playback.StartAndRender(c.Bot, slashCtx, slashCtx.AppLog, target, added)
	return nil
}
