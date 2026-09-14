package cmdadapter

import "strings"

// SlashCommandPath builds a space-separated command path from the arguments an
// invocation arrived with. Example: "settings announce channel-set" or
// "purge now".
func SlashCommandPath(commandName string, options []SlashArgument) string {
	parts := appendSlashOptions([]string{commandName}, options)
	return strings.Join(parts, " ")
}

func appendSlashOptions(parts []string, options []SlashArgument) []string {
	if len(options) == 0 {
		return parts
	}
	opt := options[0]
	switch opt.Type {
	case OptionSubCommand, OptionSubCommandGroup:
		parts = append(parts, opt.Name)
		return appendSlashOptions(parts, opt.Options)
	default:
		return parts
	}
}
