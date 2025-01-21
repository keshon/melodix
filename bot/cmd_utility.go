package bot

import (
	"github.com/bwmarrin/discordgo"
	embed "github.com/keshon/melodix/third_party/discord_embed"
)

func init() {
	registerCommand("ping", pingCommand)
	registerCommand("cache", cacheCommand)
	registerCommand("set-prefix", setPrefixCommand)

}

func pingCommand(s *discordgo.Session, m *discordgo.MessageCreate, b *Bot, command, param string) {
	s.ChannelMessageSendEmbed(m.ChannelID, embed.NewEmbed().SetColor(embedColor).SetDescription("Pong!").MessageEmbed)
}

func cacheCommand(s *discordgo.Session, m *discordgo.MessageCreate, b *Bot, command, param string) {
	if param == "on" {
		b.storage.EnableCache(m.GuildID)
		s.ChannelMessageSendEmbed(m.ChannelID, embed.NewEmbed().SetColor(embedColor).SetDescription("Cache enabled").MessageEmbed)
	} else if param == "off" {
		b.storage.DisableCache(m.GuildID)
		s.ChannelMessageSendEmbed(m.ChannelID, embed.NewEmbed().SetColor(embedColor).SetDescription("Cache disabled").MessageEmbed)
	} else {
		s.ChannelMessageSendEmbed(m.ChannelID, embed.NewEmbed().SetColor(embedColor).SetDescription("Invalid parameter, use `cache on` or `cache off`").MessageEmbed)
	}
}

func setPrefixCommand(s *discordgo.Session, m *discordgo.MessageCreate, b *Bot, command, param string) {
	emb := embed.NewEmbed().SetColor(embedColor)
	if len(param) == 0 {
		s.ChannelMessageSendEmbed(m.ChannelID, emb.SetDescription("Please provide a prefix.").MessageEmbed)
		return
	}
	b.prefixCache[m.GuildID] = param
	err := b.storage.SavePrefix(m.GuildID, param)
	if err != nil {
		s.ChannelMessageSendEmbed(m.ChannelID, emb.SetDescription("Error saving new prefix.").MessageEmbed)
		return
	}
	s.ChannelMessageSendEmbed(m.ChannelID, emb.SetDescription("Prefix changed to `"+param+"`\nUse `"+param+"help` for a list of commands.").MessageEmbed)
}
