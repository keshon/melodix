package about

import (
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"

	"github.com/keshon/melodix/internal/discord/adapter"
	"github.com/keshon/melodix/internal/discord/reply"
)

type Command struct{}

func (c *Command) Name() string        { return "about" }
func (c *Command) Description() string { return "Discover the origin of this bot" }
func (c *Command) Group() string       { return "core" }
func (c *Command) Category() string    { return "ℹ️ Information" }
func (c *Command) UserPermissions() []int64 {
	return []int64{}
}

func (c *Command) SlashDefinition() *adapter.SlashCommand {
	return &adapter.SlashCommand{
		Name:        c.Name(),
		Description: c.Description(),
	}
}

func (c *Command) Run(context *adapter.SlashInteractionContext) error {

	fields := []adapter.EmbedField{
		{
			Name:  "Developed by Señor Mega",
			Value: "[LinkedIn](https://www.linkedin.com/in/keshon), [GitHub](https://github.com/keshon), [Homepage](https://keshon.ru)",
		},
		{
			Name:  "Repository",
			Value: "https://github.com/keshon/melodix",
		},
		{
			Name:  "Runtime",
			Value: runtime.Version(),
		},
	}

	build, ok := debug.ReadBuildInfo()
	if ok {
		for _, setting := range build.Settings {
			if setting.Key == "vcs.revision" && setting.Value != "" {
				fields = append(fields, adapter.EmbedField{Name: "Commit", Value: setting.Value})
				break
			}
		}
	}

	embed := &adapter.Embed{
		Title:       "ℹ️ About Melodix",
		Description: "Music player for Discord and the terminal.",
		Color:       reply.EmbedColor,
		Fields:      fields,
	}

	// The banner is an optional asset: a deployment that ships only the binary
	// still gets the embed, just without the image.
	imagePath := "./assets/about-banner.webp"
	if f, err := os.Open(imagePath); err == nil {
		defer f.Close()
		imageName := filepath.Base(imagePath)
		embed.ImageURL = "attachment://" + imageName
		return context.RespondWith(adapter.Reply{
			Embed: embed, File: f, FileName: imageName, Ephemeral: true,
		})
	}

	return context.RespondEphemeral(embed)
}
