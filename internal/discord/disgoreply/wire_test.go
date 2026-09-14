package disgoreply

import (
	"testing"

	"github.com/disgoorg/disgo/discord"

	"github.com/keshon/melodix/internal/discord/cmdadapter"
)

func strPtr(s string) *string { return &s }

func rawOption(name string, t discord.ApplicationCommandOptionType, raw string) discord.SlashCommandOption {
	return discord.SlashCommandOption{Name: name, Type: t, Value: []byte(raw)}
}

// The two libraries disagree about shape here and the commands only know one
// of them. discordgo nests a group inside a subcommand inside its arguments;
// disgo resolves that before we see it and hands over the names separately
// with a flat map of leaves.
//
// /settings commands enable <group> is the case that matters: routing asks for
// FirstOption, then First, then Option by name. If the tree is not rebuilt,
// every one of those returns nothing and the subcommand silently stops being
// found -- with no error, because "no such option" is a legitimate answer.
func TestSubcommandGroupsAreRebuiltIntoATree(t *testing.T) {
	data := discord.SlashCommandInteractionData{
		SubCommandGroupName: strPtr("commands"),
		SubCommandName:      strPtr("enable"),
		Options: map[string]discord.SlashCommandOption{
			"group": rawOption("group", discord.ApplicationCommandOptionTypeString, `"music"`),
		},
	}

	args := SlashArguments(data)
	ctx := &cmdadapter.SlashInteractionContext{Arguments: args}

	group, ok := ctx.FirstOption()
	if !ok || group.Name != "commands" || group.Type != cmdadapter.OptionSubCommandGroup {
		t.Fatalf("group = %+v", group)
	}
	sub, ok := group.First()
	if !ok || sub.Name != "enable" || sub.Type != cmdadapter.OptionSubCommand {
		t.Fatalf("subcommand = %+v", sub)
	}
	arg, ok := sub.Option("group")
	if !ok || arg.StringValue() != "music" {
		t.Fatalf("argument = %+v", arg)
	}
}

// A subcommand with no group above it is the shallower case -- /purge now --
// and must not grow an invented group to sit in.
func TestBareSubcommandHasNoGroupAboveIt(t *testing.T) {
	data := discord.SlashCommandInteractionData{
		SubCommandName: strPtr("now"),
	}

	args := SlashArguments(data)
	if len(args) != 1 {
		t.Fatalf("args = %+v, want one", args)
	}
	if args[0].Name != "now" || args[0].Type != cmdadapter.OptionSubCommand {
		t.Errorf("subcommand = %+v", args[0])
	}
}

// A plain command's arguments arrive at the top level with nothing above them.
func TestPlainArgumentsStayAtTheTopLevel(t *testing.T) {
	data := discord.SlashCommandInteractionData{
		Options: map[string]discord.SlashCommandOption{
			"input":  rawOption("input", discord.ApplicationCommandOptionTypeString, `"never gonna give"`),
			"source": rawOption("source", discord.ApplicationCommandOptionTypeString, `"youtube"`),
		},
	}

	ctx := &cmdadapter.SlashInteractionContext{Arguments: SlashArguments(data)}
	if got := ctx.StringOption("input"); got != "never gonna give" {
		t.Errorf("input = %q", got)
	}
	if got := ctx.StringOption("source"); got != "youtube" {
		t.Errorf("source = %q", got)
	}
}

// disgo keeps values as raw JSON, so every type has to be decoded rather than
// asserted. An integer decoded as the wrong type reads back as zero, which is
// a legitimate page number and so would not look like a failure.
func TestValuesDecodeToTheDeclaredType(t *testing.T) {
	data := discord.SlashCommandInteractionData{
		Options: map[string]discord.SlashCommandOption{
			"page":    rawOption("page", discord.ApplicationCommandOptionTypeInt, `7`),
			"shuffle": rawOption("shuffle", discord.ApplicationCommandOptionTypeBool, `true`),
			"input":   rawOption("input", discord.ApplicationCommandOptionTypeString, `"x"`),
		},
	}

	ctx := &cmdadapter.SlashInteractionContext{Arguments: SlashArguments(data)}
	if got := ctx.IntOption("page"); got != 7 {
		t.Errorf("page = %d, want 7", got)
	}
	shuffle, _ := ctx.Option("shuffle")
	if !shuffle.BoolValue() {
		t.Error("shuffle decoded as false")
	}
	if got := ctx.StringOption("input"); got != "x" {
		t.Errorf("input = %q", got)
	}
}

// An empty invocation is a real case -- /settings with no group -- and has to
// answer rather than panic.
func TestEmptyInvocationAnswersEmpty(t *testing.T) {
	ctx := &cmdadapter.SlashInteractionContext{
		Arguments: SlashArguments(discord.SlashCommandInteractionData{}),
	}
	if opts := ctx.Options(); len(opts) != 0 {
		t.Errorf("options = %v, want none", opts)
	}
	if _, ok := ctx.FirstOption(); ok {
		t.Error("found a first option in an empty invocation")
	}
}

// Go randomises map iteration and disgo hands the leaves over as a map. The
// order is not read positionally, but a slice whose order changes between two
// runs of the same command makes a later report unreproducible.
func TestLeafOrderIsStable(t *testing.T) {
	data := discord.SlashCommandInteractionData{
		Options: map[string]discord.SlashCommandOption{
			"zulu":    rawOption("zulu", discord.ApplicationCommandOptionTypeString, `"z"`),
			"alpha":   rawOption("alpha", discord.ApplicationCommandOptionTypeString, `"a"`),
			"mike":    rawOption("mike", discord.ApplicationCommandOptionTypeString, `"m"`),
			"bravo":   rawOption("bravo", discord.ApplicationCommandOptionTypeString, `"b"`),
			"charlie": rawOption("charlie", discord.ApplicationCommandOptionTypeString, `"c"`),
		},
	}

	first := SlashArguments(data)
	for i := 0; i < 20; i++ {
		again := SlashArguments(data)
		for j := range first {
			if first[j].Name != again[j].Name {
				t.Fatalf("order changed between runs: %q then %q at %d",
					first[j].Name, again[j].Name, j)
			}
		}
	}
}

// The three command kinds are three types in disgo rather than one struct with
// a Type field. Registering one as the wrong kind is accepted by Discord and
// then unusable, with no error to notice.
func TestCommandKindsRenderToTheirOwnTypes(t *testing.T) {
	chat := SlashCommandCreate(&cmdadapter.SlashCommand{Name: "play", Description: "play"})
	if _, ok := chat.(discord.SlashCommandCreate); !ok {
		t.Errorf("a chat-input command rendered as %T", chat)
	}
	menu := SlashCommandCreate(&cmdadapter.SlashCommand{
		Type: cmdadapter.MessageMenuCommand, Name: "Add to queue",
	})
	if _, ok := menu.(discord.MessageCommandCreate); !ok {
		t.Errorf("a message-menu command rendered as %T", menu)
	}
	if SlashCommandCreate(nil) != nil {
		t.Error("a nil declaration rendered as something")
	}
}

// Nesting is typed per level in disgo: a group holds subcommands, a subcommand
// holds plain arguments. Getting it wrong is a registration Discord rejects.
func TestNestedDeclarationsRenderAtEachLevel(t *testing.T) {
	def := &cmdadapter.SlashCommand{
		Name:        "settings",
		Description: "settings",
		Options: []cmdadapter.SlashOption{{
			Type: cmdadapter.OptionSubCommandGroup, Name: "commands", Description: "g",
			Options: []cmdadapter.SlashOption{{
				Type: cmdadapter.OptionSubCommand, Name: "enable", Description: "s",
				Options: []cmdadapter.SlashOption{{
					Type: cmdadapter.OptionString, Name: "group", Description: "a", Required: true,
				}},
			}},
		}},
	}

	created, ok := SlashCommandCreate(def).(discord.SlashCommandCreate)
	if !ok {
		t.Fatal("settings did not render as a slash command")
	}
	group, ok := created.Options[0].(discord.ApplicationCommandOptionSubCommandGroup)
	if !ok {
		t.Fatalf("group rendered as %T", created.Options[0])
	}
	if len(group.Options) != 1 || group.Options[0].Name != "enable" {
		t.Fatalf("subcommand = %+v", group.Options)
	}
	arg, ok := group.Options[0].Options[0].(discord.ApplicationCommandOptionString)
	if !ok {
		t.Fatalf("argument rendered as %T", group.Options[0].Options[0])
	}
	if arg.Name != "group" || !arg.Required {
		t.Errorf("argument = %+v", arg)
	}
}

// Consuming a chooser means sending an empty component list, which is a
// different message from one that never had components.
func TestNoRowsRendersAnEmptyListRatherThanNil(t *testing.T) {
	rows := Components(nil)
	if rows == nil {
		t.Fatal("nil rows rendered as nil; consuming a chooser needs an empty list")
	}
	if len(rows) != 0 {
		t.Fatalf("got %d rows, want none", len(rows))
	}
}

// The chooser puts everything it needs into the button id, so the id is the
// one field that must survive intact -- lose it and a click means nothing.
func TestButtonsKeepTheirCustomID(t *testing.T) {
	rows := Components([]cmdadapter.ActionRow{{Buttons: []cmdadapter.Button{
		{Label: "1", CustomID: "search:yt:abc123"},
		{Label: "2", CustomID: "search:sc:def456", Disabled: true},
	}}})

	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	row, ok := rows[0].(discord.ActionRowComponent)
	if !ok {
		t.Fatalf("row is %T", rows[0])
	}
	first, ok := row.Components[0].(discord.ButtonComponent)
	if !ok {
		t.Fatalf("button is %T", row.Components[0])
	}
	if first.CustomID != "search:yt:abc123" || first.Label != "1" {
		t.Errorf("first button = %+v", first)
	}
	if second := row.Components[1].(discord.ButtonComponent); !second.Disabled {
		t.Errorf("second button lost its disabled state: %+v", second)
	}
	if first.Style != discord.ButtonStyleSecondary {
		t.Errorf("default style = %v, want secondary", first.Style)
	}
}

// An embed's fields carry Inline per field, and disgo takes it as a pointer.
// Taking the address of a range variable is the classic way to give every
// field the last one's value.
func TestEmbedFieldsKeepTheirOwnInlineFlag(t *testing.T) {
	e := Embed(&cmdadapter.Embed{
		Title: "t", Description: "d", Footer: "f", ImageURL: "http://x/y.png",
		Fields: []cmdadapter.EmbedField{
			{Name: "a", Value: "1", Inline: true},
			{Name: "b", Value: "2", Inline: false},
			{Name: "c", Value: "3", Inline: true},
		},
	})

	want := []bool{true, false, true}
	for i, f := range e.Fields {
		if f.Inline == nil {
			t.Fatalf("field %d has no inline flag", i)
		}
		if *f.Inline != want[i] {
			t.Errorf("field %q inline = %v, want %v", f.Name, *f.Inline, want[i])
		}
	}
	if e.Footer == nil || e.Footer.Text != "f" {
		t.Errorf("footer = %+v", e.Footer)
	}
	if e.Image == nil || e.Image.URL != "http://x/y.png" {
		t.Errorf("image = %+v", e.Image)
	}
}

// A nil embed must not become a blank one that gets sent.
func TestNilEmbedSendsNothing(t *testing.T) {
	if got := Embeds(nil); len(got) != 0 {
		t.Errorf("nil embed rendered as %d embeds", len(got))
	}
}
