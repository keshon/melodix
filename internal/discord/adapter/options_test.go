package adapter

import (
	"testing"
)

func invocation(opts ...SlashArgument) *SlashInteractionContext {
	return &SlashInteractionContext{Arguments: opts}
}

func opt(name string, t SlashOptionType, v any, sub ...SlashArgument) SlashArgument {
	return SlashArgument{Name: name, Type: t, Value: v, Options: sub}
}

// The commands used to loop over the raw options with a switch on the name.
// Asking for one by name is the same question with less to get wrong.
func TestStringOptionReadsArgumentsByName(t *testing.T) {
	c := invocation(
		opt("input", OptionString, "never gonna give"),
		opt("source", OptionString, "youtube"),
	)

	if got := c.StringOption("input"); got != "never gonna give" {
		t.Errorf("input = %q", got)
	}
	if got := c.StringOption("source"); got != "youtube" {
		t.Errorf("source = %q", got)
	}
}

// Absent and empty are the same answer to "what did they type", and every
// caller here treats them that way. An optional argument must not need a
// presence check before it can be read.
func TestMissingOptionsReadAsZero(t *testing.T) {
	c := invocation(opt("input", OptionString, "x"))

	if got := c.StringOption("nothing"); got != "" {
		t.Errorf("missing string = %q, want empty", got)
	}
	if got := c.IntOption("nothing"); got != 0 {
		t.Errorf("missing int = %d, want 0", got)
	}
	if _, ok := c.Option("nothing"); ok {
		t.Error("reported a missing argument as present")
	}
}

// Discord sends whole numbers as float64 over JSON. The history page argument
// is the one that cares, and reading it as an int64 type assertion would
// quietly give every page as zero.
func TestIntOptionAcceptsWhatJSONActuallyDelivers(t *testing.T) {
	c := invocation(opt("page", OptionInteger, float64(7)))

	if got := c.IntOption("page"); got != 7 {
		t.Fatalf("page = %d, want 7", got)
	}
}

// /settings commands enable <group> is a group holding a subcommand holding an
// argument. Routing walks it by name at each level.
func TestNestedSubcommandsAreWalkable(t *testing.T) {
	c := invocation(
		opt("commands", OptionSubCommandGroup, nil,
			opt("enable", OptionSubCommand, nil,
				opt("group", OptionString, "music"))),
	)

	group, ok := c.FirstOption()
	if !ok || group.Name != "commands" || group.Type != OptionSubCommandGroup {
		t.Fatalf("group = %+v", group)
	}
	sub, ok := group.First()
	if !ok || sub.Name != "enable" || sub.Type != OptionSubCommand {
		t.Fatalf("subcommand = %+v", sub)
	}
	arg, ok := sub.Option("group")
	if !ok || arg.StringValue() != "music" {
		t.Fatalf("argument = %+v", arg)
	}
}

// An invocation with nothing in it is a real case -- /settings with no group --
// and it has to answer rather than panic.
func TestEmptyInvocationAnswersEmpty(t *testing.T) {
	c := invocation()

	if opts := c.Options(); len(opts) != 0 {
		t.Errorf("options = %v, want none", opts)
	}
	if _, ok := c.FirstOption(); ok {
		t.Error("found a first option in an empty invocation")
	}
	if _, ok := (SlashArgument{}).First(); ok {
		t.Error("found a nested option in an empty argument")
	}
}

// The path is what an audit row names when a command is a tree of
// subcommands: the leaf is what the caller actually ran.
func TestSlashCommandPathNamesTheSubcommandRun(t *testing.T) {
	args := []SlashArgument{
		opt("announce", OptionSubCommandGroup, nil,
			opt("channel-set", OptionSubCommand, nil,
				opt("channel", OptionString, "123"))),
	}

	if got := SlashCommandPath("settings", args); got != "settings announce channel-set" {
		t.Errorf("path = %q", got)
	}
	if got := SlashCommandPath("ping", nil); got != "ping" {
		t.Errorf("bare command path = %q, want the command name", got)
	}
}
