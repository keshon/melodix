package cmdadapter

// SlashArgument is one argument as it arrived, or one subcommand carrying its
// own. Discord models both as options, which is why this is one type.
//
// The accessors are named after discordgo's because that is what the commands
// already said, and because StringValue reads better than String would: a
// method called String on an exported struct makes it a fmt.Stringer by
// accident, and then every log line that prints an argument prints something
// nobody chose.
type SlashArgument struct {
	Name    string
	Type    SlashOptionType
	Value   any
	Options []SlashArgument
}

// StringValue is the empty string when the argument was not given, which is
// what every caller here wants: absent and empty are the same answer to "what
// did they type".
func (a SlashArgument) StringValue() string {
	s, _ := a.Value.(string)
	return s
}

// IntValue is zero when the argument was not given. Discord sends whole
// numbers as float64 over JSON, so both are accepted.
func (a SlashArgument) IntValue() int64 {
	switch v := a.Value.(type) {
	case int64:
		return v
	case float64:
		return int64(v)
	default:
		return 0
	}
}

// BoolValue is false when the argument was not given.
func (a SlashArgument) BoolValue() bool {
	b, _ := a.Value.(bool)
	return b
}

// Option finds a nested argument by name -- the arguments of a subcommand, or
// the subcommands of a group.
func (a SlashArgument) Option(name string) (SlashArgument, bool) {
	return findOption(a.Options, name)
}

// First is the leading nested argument, which for a subcommand group is the
// subcommand that was actually invoked. Routing reads better as a name than as
// an index into a slice that is always length one.
func (a SlashArgument) First() (SlashArgument, bool) {
	if len(a.Options) == 0 {
		return SlashArgument{}, false
	}
	return a.Options[0], true
}

func findOption(opts []SlashArgument, name string) (SlashArgument, bool) {
	for _, o := range opts {
		if o.Name == name {
			return o, true
		}
	}
	return SlashArgument{}, false
}

// --- reading them off an invocation ---

// Options are the arguments this command was invoked with, resolved when the
// context was built.
func (c *SlashInteractionContext) Options() []SlashArgument {
	return c.Arguments
}

// Option finds a top-level argument by name.
func (c *SlashInteractionContext) Option(name string) (SlashArgument, bool) {
	return findOption(c.Options(), name)
}

// StringOption is the common case: an optional string, empty when absent.
func (c *SlashInteractionContext) StringOption(name string) string {
	opt, _ := c.Option(name)
	return opt.StringValue()
}

// IntOption is the same for a whole number.
func (c *SlashInteractionContext) IntOption(name string) int64 {
	opt, _ := c.Option(name)
	return opt.IntValue()
}

// FirstOption is the leading argument, which for a command built out of
// subcommand groups is the group that was invoked.
func (c *SlashInteractionContext) FirstOption() (SlashArgument, bool) {
	opts := c.Options()
	if len(opts) == 0 {
		return SlashArgument{}, false
	}
	return opts[0], true
}
