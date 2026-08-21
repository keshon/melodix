package about

import (
	"os"
	"path/filepath"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/buildinfo"
	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/discord/reply"
)

type About struct{}

func (c *About) Name() string        { return "about" }
func (c *About) Description() string { return "Discover the origin of this bot" }
func (c *About) Group() string       { return "core" }
func (c *About) Category() string    { return "ℹ️ Information" }
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
	context, ok := ctx.(*cmdadapter.SlashInteractionContext)
	if !ok {
		return nil
	}

	session := context.Session
	event := context.Event

	info := buildinfo.Get()

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

	embed := &discordgo.MessageEmbed{
		Title:       "ℹ️ About " + info.Project,
		Description: info.Description,
		Color:       reply.EmbedColor,
		Fields:      fields,
	}

	// The banner is an optional asset: a deployment that ships only the binary
	// still gets the embed, just without the image.
	imagePath := "./assets/about-banner.webp"
	if f, err := os.Open(imagePath); err == nil {
		defer f.Close()
		imageName := filepath.Base(imagePath)
		embed.Image = &discordgo.MessageEmbedImage{URL: "attachment://" + imageName}
		return reply.RespondEmbedEphemeralWithFile(session, event, embed, f, imageName)
	}

	reply.RespondEmbedEphemeral(session, event, embed)

	return nil
}
