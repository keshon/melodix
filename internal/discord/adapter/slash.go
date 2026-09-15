package adapter

// SlashCommand is how a command declares itself to Discord, in terms a command
// can write without importing the library that registers it.
//
// The Adapter forwards it and registration renders it, so a command never
// names the library that will receive the declaration.
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
