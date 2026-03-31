package core

import (
	"os"
	"path/filepath"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/buildinfo"
	"github.com/keshon/melodix/internal/command"
	"github.com/keshon/melodix/internal/discord"
)

type About struct{}

func (c *About) Name() string        { return "about" }
func (c *About) Description() string { return "Discover the origin of this bot" }
func (c *About) Group() string       { return "core" }
func (c *About) Category() string    { return "🕯️ Information" }
func (c *About) UserPermissions() []int64 {
	return []int64{}
}

func (c *About) SlashDefinition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        c.Name(),
		Description: c.Description(),
	}
}

func (c *About) Run(ctx interface{}) error {
	context, ok := ctx.(*command.SlashInteractionContext)
	if !ok {
		return nil
	}

	session := context.Session
	event := context.Event

	info := buildinfo.Get()

	// Info fields for embed
	fields := []*discordgo.MessageEmbedField{
		{
			Name:  "Developed by Señor Mega",
			Value: "[LinkedIn](https://www.linkedin.com/in/keshon), [GitHub](https://github.com/keshon), [Homepage](https://keshon.ru)",
		},
		{
			Name:  "Repository",
			Value: "https://github.com/keshon/melodix\nCommit: " + info.Commit,
		},
		{
			Name:  "Release",
			Value: info.BuildTime + " (" + info.GoVersion + ")",
		},
	}

	// Create embed
	embed := &discordgo.MessageEmbed{
		Title:       "ℹ️ About " + info.Project,
		Description: info.Description,
		Color:       discord.EmbedColor,
		Fields:      fields,
	}

	// Try attaching banner if exists
	imagePath := "./assets/about-banner.webp"
	if f, err := os.Open(imagePath); err == nil {
		defer f.Close()
		imageName := filepath.Base(imagePath)
		embed.Image = &discordgo.MessageEmbedImage{URL: "attachment://" + imageName}
		return discord.RespondEmbedEphemeralWithFile(session, event, embed, f, imageName)
	}

	// Just embed if no banner
	discord.RespondEmbedEphemeral(session, event, embed)

	return nil
}
