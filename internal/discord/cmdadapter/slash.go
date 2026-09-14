package cmdadapter

import "github.com/bwmarrin/discordgo"

// SlashCommand is how a command declares itself to Discord, in terms a command
// can write without importing the library that registers it.
//
// The translation is at the bottom of this file and the Adapter is where it
// happens, so cmdsync keeps receiving exactly what it received before and only
// the command side changes.
type SlashCommand struct {
	// Type defaults to a chat-input command, which is what every command here
	// is. The context-menu kinds exist because the interface that carries them
	// does.
	Type        SlashCommandType
	Name        string
	Description string
	Options     []SlashOption
}

// SlashOption is one argument, or one subcommand. Discord models both as
// options, which is why a subcommand carries its own Options.
type SlashOption struct {
	Type        SlashOptionType
	Name        string
	Description string
	Required    bool
	// Choices constrain a value to a fixed set, shown as a picker.
	Choices []SlashChoice
	// Options are the arguments of a subcommand, or the subcommands of a
	// group.
	Options []SlashOption
	// MinValue bounds a numeric option from below. A pointer because zero is
	// a legitimate minimum and "unset" has to be distinguishable from it.
	MinValue *float64
	// MaxValue bounds it from above. Zero means unbounded, which is how
	// Discord reads it.
	MaxValue float64
}

// SlashChoice is one entry in a picker. Value is what arrives back.
type SlashChoice struct {
	Name  string
	Value any
}

// SlashCommandType distinguishes a typed command from the two context-menu
// entries. Zero is a chat-input command because that is what almost every
// declaration is, and it should not have to say so.
type SlashCommandType int

const (
	ChatInputCommand SlashCommandType = iota
	MessageMenuCommand
	UserMenuCommand
)

// SlashOptionType is the kind of an option.
//
// The values are this package's own rather than Discord's, and the mapping
// below is explicit, so a change to either numbering is a compile error or a
// missed case here rather than a command that silently registers as the wrong
// kind of argument.
type SlashOptionType int

const (
	OptionSubCommand SlashOptionType = iota + 1
	OptionSubCommandGroup
	OptionString
	OptionInteger
	OptionBoolean
)

// DiscordSlashCommand renders a declaration into the wire format. Exported
// because cmdsync registers straight off the command rather than through the
// Adapter, so both need the same translation.
func DiscordSlashCommand(c *SlashCommand) *discordgo.ApplicationCommand {
	if c == nil {
		return nil
	}
	return &discordgo.ApplicationCommand{
		Type:        discordCommandType(c.Type),
		Name:        c.Name,
		Description: c.Description,
		Options:     discordOptions(c.Options),
	}
}

func discordCommandType(t SlashCommandType) discordgo.ApplicationCommandType {
	switch t {
	case MessageMenuCommand:
		return discordgo.MessageApplicationCommand
	case UserMenuCommand:
		return discordgo.UserApplicationCommand
	default:
		return discordgo.ChatApplicationCommand
	}
}

func discordOptions(opts []SlashOption) []*discordgo.ApplicationCommandOption {
	if len(opts) == 0 {
		return nil
	}
	out := make([]*discordgo.ApplicationCommandOption, 0, len(opts))
	for _, o := range opts {
		out = append(out, &discordgo.ApplicationCommandOption{
			Type:        discordOptionType(o.Type),
			Name:        o.Name,
			Description: o.Description,
			Required:    o.Required,
			Choices:     discordChoices(o.Choices),
			Options:     discordOptions(o.Options),
			MinValue:    o.MinValue,
			MaxValue:    o.MaxValue,
		})
	}
	return out
}

func discordOptionType(t SlashOptionType) discordgo.ApplicationCommandOptionType {
	switch t {
	case OptionSubCommand:
		return discordgo.ApplicationCommandOptionSubCommand
	case OptionSubCommandGroup:
		return discordgo.ApplicationCommandOptionSubCommandGroup
	case OptionInteger:
		return discordgo.ApplicationCommandOptionInteger
	case OptionBoolean:
		return discordgo.ApplicationCommandOptionBoolean
	default:
		return discordgo.ApplicationCommandOptionString
	}
}

func discordChoices(choices []SlashChoice) []*discordgo.ApplicationCommandOptionChoice {
	if len(choices) == 0 {
		return nil
	}
	out := make([]*discordgo.ApplicationCommandOptionChoice, 0, len(choices))
	for _, c := range choices {
		out = append(out, &discordgo.ApplicationCommandOptionChoice{
			Name:  c.Name,
			Value: c.Value,
		})
	}
	return out
}
