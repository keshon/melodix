package bot

import (
	"github.com/bwmarrin/discordgo"
	embed "github.com/keshon/melodix/third_party/discord_embed"
)

func init() {
	registerCommand("ping", pingCommand)
	registerCommand("set-prefix", setPrefixCommand)
}

func pingCommand(s *discordgo.Session, m *discordgo.MessageCreate, b *Bot, command, param string) {
	s.ChannelMessageSendEmbed(m.ChannelID, embed.NewEmbed().SetColor(embedColor).SetDescription("Pong!").MessageEmbed)
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
