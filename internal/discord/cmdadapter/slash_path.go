package cmdadapter

import (
	"strings"

	"github.com/bwmarrin/discordgo"
)

// SlashCommandPath builds a space-separated command path from slash interaction options.
// Example: "settings announce channel-set" or "purge now".
func SlashCommandPath(commandName string, options []*discordgo.ApplicationCommandInteractionDataOption) string {
	parts := appendSlashOptions([]string{commandName}, options)
	return strings.Join(parts, " ")
}

func appendSlashOptions(parts []string, options []*discordgo.ApplicationCommandInteractionDataOption) []string {
	if len(options) == 0 {
		return parts
	}
	opt := options[0]
	switch opt.Type {
	case discordgo.ApplicationCommandOptionSubCommand, discordgo.ApplicationCommandOptionSubCommandGroup:
		parts = append(parts, opt.Name)
		return appendSlashOptions(parts, opt.Options)
	default:
		return parts
	}
}
