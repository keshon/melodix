// Package disgoreply is the disgo half of what reply is for discordgo: it
// renders cmdadapter's neutral types into disgo's wire types, and implements
// the reply surface a command works with.
//
// It is a sibling of internal/discord/reply, not a layer over it. The two do
// not share code and are not meant to: the whole point of the seam is that a
// backend is one package implementing two interfaces, so deleting a backend is
// deleting a package.
package disgoreply

import (
	"encoding/json"
	"sort"

	"github.com/disgoorg/disgo/discord"

	"github.com/keshon/melodix/internal/discord/cmdadapter"
)

// Embed renders an embed. A nil Embed renders as the zero value, which the
// callers here never send: they check first, because disgo takes embeds by
// value and there is no nil to pass through.
func Embed(e *cmdadapter.Embed) discord.Embed {
	if e == nil {
		return discord.Embed{}
	}

	out := discord.Embed{
		Title:       e.Title,
		Description: e.Description,
		Color:       e.Color,
	}
	if e.Footer != "" {
		out.Footer = &discord.EmbedFooter{Text: e.Footer}
	}
	if e.ImageURL != "" {
		out.Image = &discord.EmbedResource{URL: e.ImageURL}
	}
	for _, f := range e.Fields {
		out.Fields = append(out.Fields, discord.EmbedField{
			Name:   f.Name,
			Value:  f.Value,
			Inline: &f.Inline,
		})
	}
	return out
}

// Embeds wraps one embed as the slice every send takes, or an empty slice for
// a nil embed so a caller cannot accidentally send a blank one.
func Embeds(e *cmdadapter.Embed) []discord.Embed {
	if e == nil {
		return nil
	}
	return []discord.Embed{Embed(e)}
}

// Components renders rows of controls. Empty stays empty: sending an empty
// component list is how a chooser is consumed, and that is different from
// sending none at all.
func Components(rows []cmdadapter.ActionRow) []discord.LayoutComponent {
	out := make([]discord.LayoutComponent, 0, len(rows))
	for _, row := range rows {
		buttons := make([]discord.InteractiveComponent, 0, len(row.Buttons))
		for _, b := range row.Buttons {
			buttons = append(buttons, discord.ButtonComponent{
				Label:    b.Label,
				Style:    buttonStyle(b.Style),
				CustomID: b.CustomID,
				Disabled: b.Disabled,
			})
		}
		out = append(out, discord.NewActionRow(buttons...))
	}
	return out
}

func buttonStyle(s cmdadapter.ButtonStyle) discord.ButtonStyle {
	switch s {
	case cmdadapter.PrimaryButton:
		return discord.ButtonStylePrimary
	case cmdadapter.SuccessButton:
		return discord.ButtonStyleSuccess
	case cmdadapter.DangerButton:
		return discord.ButtonStyleDanger
	default:
		return discord.ButtonStyleSecondary
	}
}

// SlashCommandCreate renders a declaration into the form registration sends.
//
// disgo models the three command kinds as three types behind an interface
// rather than one struct with a Type field, which matches
// cmdadapter.SlashCommandType's own three-way split -- so this returns the
// interface and the switch is the whole of the translation.
func SlashCommandCreate(c *cmdadapter.SlashCommand) discord.ApplicationCommandCreate {
	if c == nil {
		return nil
	}
	switch c.Type {
	case cmdadapter.MessageMenuCommand:
		return discord.MessageCommandCreate{Name: c.Name}
	case cmdadapter.UserMenuCommand:
		return discord.UserCommandCreate{Name: c.Name}
	default:
		return discord.SlashCommandCreate{
			Name:        c.Name,
			Description: c.Description,
			Options:     options(c.Options),
		}
	}
}

func options(opts []cmdadapter.SlashOption) []discord.ApplicationCommandOption {
	if len(opts) == 0 {
		return nil
	}
	out := make([]discord.ApplicationCommandOption, 0, len(opts))
	for _, o := range opts {
		out = append(out, option(o))
	}
	return out
}

// option renders one argument. disgo types options the same way it types
// commands -- one Go type per kind -- so an option that this package does not
// model cannot be built by accident; it falls to a string, which is what an
// unrecognised argument arrives as anyway.
func option(o cmdadapter.SlashOption) discord.ApplicationCommandOption {
	switch o.Type {
	case cmdadapter.OptionSubCommand:
		return discord.ApplicationCommandOptionSubCommand{
			Name:        o.Name,
			Description: o.Description,
			Options:     subOptions(o.Options),
		}
	case cmdadapter.OptionSubCommandGroup:
		return discord.ApplicationCommandOptionSubCommandGroup{
			Name:        o.Name,
			Description: o.Description,
			Options:     subCommands(o.Options),
		}
	case cmdadapter.OptionInteger:
		opt := discord.ApplicationCommandOptionInt{
			Name:        o.Name,
			Description: o.Description,
			Required:    o.Required,
			Choices:     intChoices(o.Choices),
		}
		if o.MinValue != nil {
			min := int(*o.MinValue)
			opt.MinValue = &min
		}
		if o.MaxValue != 0 {
			max := int(o.MaxValue)
			opt.MaxValue = &max
		}
		return opt
	case cmdadapter.OptionBoolean:
		return discord.ApplicationCommandOptionBool{
			Name:        o.Name,
			Description: o.Description,
			Required:    o.Required,
		}
	default:
		return discord.ApplicationCommandOptionString{
			Name:        o.Name,
			Description: o.Description,
			Required:    o.Required,
			Choices:     stringChoices(o.Choices),
		}
	}
}

// subCommands and subOptions narrow to the types disgo requires at each
// nesting level: a group holds subcommands, a subcommand holds plain
// arguments. Anything at the wrong level is dropped rather than coerced --
// Discord would reject the registration, and inventing a shape here would
// register a command nobody declared.
func subCommands(opts []cmdadapter.SlashOption) []discord.ApplicationCommandOptionSubCommand {
	out := make([]discord.ApplicationCommandOptionSubCommand, 0, len(opts))
	for _, o := range opts {
		if o.Type != cmdadapter.OptionSubCommand {
			continue
		}
		out = append(out, discord.ApplicationCommandOptionSubCommand{
			Name:        o.Name,
			Description: o.Description,
			Options:     subOptions(o.Options),
		})
	}
	return out
}

func subOptions(opts []cmdadapter.SlashOption) []discord.ApplicationCommandOption {
	out := make([]discord.ApplicationCommandOption, 0, len(opts))
	for _, o := range opts {
		if o.Type == cmdadapter.OptionSubCommand || o.Type == cmdadapter.OptionSubCommandGroup {
			continue
		}
		out = append(out, option(o))
	}
	return out
}

func stringChoices(choices []cmdadapter.SlashChoice) []discord.ApplicationCommandOptionChoiceString {
	if len(choices) == 0 {
		return nil
	}
	out := make([]discord.ApplicationCommandOptionChoiceString, 0, len(choices))
	for _, c := range choices {
		v, _ := c.Value.(string)
		out = append(out, discord.ApplicationCommandOptionChoiceString{Name: c.Name, Value: v})
	}
	return out
}

func intChoices(choices []cmdadapter.SlashChoice) []discord.ApplicationCommandOptionChoiceInt {
	if len(choices) == 0 {
		return nil
	}
	out := make([]discord.ApplicationCommandOptionChoiceInt, 0, len(choices))
	for _, c := range choices {
		out = append(out, discord.ApplicationCommandOptionChoiceInt{Name: c.Name, Value: choiceInt(c.Value)})
	}
	return out
}

// choiceInt accepts both because a declaration written by hand says int and
// one that has been through JSON says float64.
func choiceInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

// SlashArguments reads the arguments off an interaction.
//
// This is where the two libraries disagree most. discordgo delivers options as
// a nested tree -- a group holding a subcommand holding its arguments -- and
// cmdadapter.SlashArgument models that tree because that is what the commands
// walk. disgo has already resolved it: the subcommand and group names are
// separate fields and Options is a flat map of the leaves that actually
// arrived.
//
// So the tree is rebuilt here rather than the commands being taught a second
// shape. /settings commands enable <group> has to answer FirstOption with the
// group either way, or routing silently stops finding its subcommand.
func SlashArguments(data discord.SlashCommandInteractionData) []cmdadapter.SlashArgument {
	leaves := leafArguments(data.Options)

	if data.SubCommandName == nil {
		return leaves
	}
	sub := cmdadapter.SlashArgument{
		Name:    *data.SubCommandName,
		Type:    cmdadapter.OptionSubCommand,
		Options: leaves,
	}
	if data.SubCommandGroupName == nil {
		return []cmdadapter.SlashArgument{sub}
	}
	return []cmdadapter.SlashArgument{{
		Name:    *data.SubCommandGroupName,
		Type:    cmdadapter.OptionSubCommandGroup,
		Options: []cmdadapter.SlashArgument{sub},
	}}
}

// leafArguments converts the flat options map.
//
// The map is sorted by name on the way out. Nothing reads leaves positionally
// -- they are looked up by name -- but Go randomises map iteration, and a
// slice whose order changes between two invocations of the same command is
// the kind of thing that makes a later bug report unreproducible.
func leafArguments(opts map[string]discord.SlashCommandOption) []cmdadapter.SlashArgument {
	if len(opts) == 0 {
		return nil
	}
	names := make([]string, 0, len(opts))
	for name := range opts {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]cmdadapter.SlashArgument, 0, len(names))
	for _, name := range names {
		o := opts[name]
		out = append(out, cmdadapter.SlashArgument{
			Name:  o.Name,
			Type:  argumentType(o.Type),
			Value: argumentValue(o),
		})
	}
	return out
}

// argumentValue decodes the raw JSON disgo keeps the value as.
//
// An integer is decoded to int64 rather than float64: SlashArgument.IntValue
// accepts both, but int64 is what the declaration says the option is, and a
// value that reads back as the type it was declared as is one less thing to
// know.
func argumentValue(o discord.SlashCommandOption) any {
	if len(o.Value) == 0 {
		return nil
	}
	switch argumentType(o.Type) {
	case cmdadapter.OptionInteger:
		var v int64
		if err := json.Unmarshal(o.Value, &v); err != nil {
			return nil
		}
		return v
	case cmdadapter.OptionBoolean:
		var v bool
		if err := json.Unmarshal(o.Value, &v); err != nil {
			return nil
		}
		return v
	default:
		var v string
		if err := json.Unmarshal(o.Value, &v); err != nil {
			return nil
		}
		return v
	}
}

// argumentType is option read backwards. Anything this package does not model
// reads as a string, matching the registration side.
func argumentType(t discord.ApplicationCommandOptionType) cmdadapter.SlashOptionType {
	switch t {
	case discord.ApplicationCommandOptionTypeSubCommand:
		return cmdadapter.OptionSubCommand
	case discord.ApplicationCommandOptionTypeSubCommandGroup:
		return cmdadapter.OptionSubCommandGroup
	case discord.ApplicationCommandOptionTypeInt:
		return cmdadapter.OptionInteger
	case discord.ApplicationCommandOptionTypeBool:
		return cmdadapter.OptionBoolean
	default:
		return cmdadapter.OptionString
	}
}

// SlashCommandUpdate renders a declaration into the form an edit sends.
//
// disgo separates create from update because Discord does: an update is a
// patch, so every field is a pointer and an omitted one means "leave it".
// Everything the declaration can express is sent, because a declaration is
// the whole intended state rather than a delta -- anything left out here
// would silently keep whatever the guild had.
func SlashCommandUpdate(c *cmdadapter.SlashCommand) discord.ApplicationCommandUpdate {
	if c == nil {
		return nil
	}
	name := c.Name
	switch c.Type {
	case cmdadapter.MessageMenuCommand:
		return discord.MessageCommandUpdate{Name: &name}
	case cmdadapter.UserMenuCommand:
		return discord.UserCommandUpdate{Name: &name}
	default:
		description := c.Description
		opts := options(c.Options)
		return discord.SlashCommandUpdate{
			Name:        &name,
			Description: &description,
			Options:     &opts,
		}
	}
}
