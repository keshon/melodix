// commands/playback.go
package bot

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/keshon/melodix/internal/version"
	embed "github.com/keshon/melodix/third_party/discord_embed"
)

func init() {
	registerCommand("about", aboutCommand)
	registerCommand("help", helpCommand)
}

func aboutCommand(s *discordgo.Session, m *discordgo.MessageCreate, b *Bot, param string, cmdName string) {
	buildDate := "unknown"
	if version.BuildDate != "" {
		t, err := time.Parse(time.RFC3339, version.BuildDate)
		if err == nil {
			buildDate = t.Format("2006-01-02")
		} else {
			buildDate = "invalid date"
		}
	}
	goVer := "unknown"
	if version.GoVersion != "" {
		goVer = version.GoVersion
	}
	imagePath := "./assets/about-banner.webp"
	imageBytes, err := os.Open(imagePath)
	if err != nil {
		fmt.Printf("Error opening image: %v", err)
	}
	emb := embed.NewEmbed().
		SetColor(embedColor).
		SetDescription(fmt.Sprintf("%v\n\n%v — %v", "ℹ️ About", version.AppName, version.AppDescription)).
		AddField("Made by Innokentiy Sokolov", "[Linkedin](https://www.linkedin.com/in/keshon), [GitHub](https://github.com/keshon), [Homepage](https://keshon.ru)").
		AddField("Repository", "https://github.com/keshon/melodix").
		AddField("Release:", buildDate+" (go version "+strings.TrimLeft(goVer, "go")+")").
		SetImage("attachment://" + filepath.Base(imagePath)).
		MessageEmbed
	s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{emb},
		Files: []*discordgo.File{
			{
				Name:   filepath.Base(imagePath),
				Reader: imageBytes,
			},
		},
	})
}

func helpCommand(s *discordgo.Session, m *discordgo.MessageCreate, b *Bot, command, param string) {
	categoryOrder := []string{"Playback", "Advanced Playback", "Information", "Utility", "General"}
	categories := make(map[string][]Command)
	prefix, err := b.getPrefixForGuild(m.GuildID)
	if err != nil {
		log.Printf("Error fetching prefix for guild %s: %v", m.GuildID, err)
		prefix = b.defaultPrefix
	}
	for _, cmd := range commands {
		categories[cmd.Category] = append(categories[cmd.Category], cmd)
	}
	var helpMsg strings.Builder
	for _, category := range categoryOrder {
		cmds, exists := categories[category]
		if !exists {
			continue
		}
		helpMsg.WriteString(fmt.Sprintf("**%s**\n", category))
		for _, cmd := range cmds {
			if cmd.Name != "melodix-reset-prefix" {
				cmd.Name = prefix + cmd.Name
			}

			helpMsg.WriteString(fmt.Sprintf("  _%s_ - %s\n", cmd.Name, cmd.Description))
		}
		helpMsg.WriteString("\n")
	}
	s.ChannelMessageSend(m.ChannelID, b.truncatListWithNewlines(helpMsg.String()))
}
