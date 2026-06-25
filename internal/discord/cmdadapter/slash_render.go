package cmdadapter

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/command"
)

// FormatCommandEntry returns a help line for a top-level command.
func FormatCommandEntry(c command.Command) string {
	return fmt.Sprintf("`%s` - %s\n", c.Name(), c.Description())
}

// AppendSlashSubcommands writes nested slash subcommand paths into sb.
func AppendSlashSubcommands(sb *strings.Builder, commandName string, options []*discordgo.ApplicationCommandOption, prefix string) {
	for _, opt := range options {
		switch opt.Type {
		case discordgo.ApplicationCommandOptionSubCommand:
			path := strings.TrimSpace(prefix + " " + opt.Name)
			fmt.Fprintf(sb, "  `/%s %s` - %s\n", commandName, path, opt.Description)
		case discordgo.ApplicationCommandOptionSubCommandGroup:
			groupPrefix := strings.TrimSpace(prefix + " " + opt.Name)
			AppendSlashSubcommands(sb, commandName, opt.Options, groupPrefix)
		}
	}
}

// FormatCommandWithSubcommands returns help text for a command including nested subcommands.
func FormatCommandWithSubcommands(c command.Command) string {
	var sb strings.Builder
	sb.WriteString(FormatCommandEntry(c))

	sp, ok := c.(SlashProvider)
	if !ok {
		return sb.String()
	}

	def := sp.SlashDefinition()
	if def == nil {
		return sb.String()
	}

	AppendSlashSubcommands(&sb, def.Name, def.Options, "")
	return sb.String()
}
