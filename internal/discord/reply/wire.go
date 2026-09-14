package reply

import (
	"github.com/bwmarrin/discordgo"

	"github.com/keshon/melodix/internal/discord/cmdadapter"
)

// Translation between cmdadapter's neutral types and discordgo's wire types.
//
// It lives here rather than in cmdadapter because cmdadapter is the half both
// libraries share: a command declares an Embed or a SlashCommand, and the
// backend that happens to be carrying the connection renders it. Keeping the
// rendering next to the calls that send it means adding a second backend adds
// a file beside this one rather than a second set of functions inside the
// neutral package.

// DiscordEmbed renders an embed. A nil Embed stays nil so the callers that
// pass one through can keep doing so.
func DiscordEmbed(e *cmdadapter.Embed) *discordgo.MessageEmbed {
	if e == nil {
		return nil
	}

	out := &discordgo.MessageEmbed{
		Title:       e.Title,
		Description: e.Description,
		Color:       e.Color,
	}
	if e.Footer != "" {
		out.Footer = &discordgo.MessageEmbedFooter{Text: e.Footer}
	}
	if e.ImageURL != "" {
		out.Image = &discordgo.MessageEmbedImage{URL: e.ImageURL}
	}
	for _, f := range e.Fields {
		out.Fields = append(out.Fields, &discordgo.MessageEmbedField{
			Name:   f.Name,
			Value:  f.Value,
			Inline: f.Inline,
		})
	}
	return out
}

// DiscordComponents renders rows of controls. Empty stays empty: sending an
// empty component list is how a chooser is consumed, and that is different
// from sending none at all.
func DiscordComponents(rows []cmdadapter.ActionRow) []discordgo.MessageComponent {
	out := make([]discordgo.MessageComponent, 0, len(rows))
	for _, row := range rows {
		buttons := make([]discordgo.MessageComponent, 0, len(row.Buttons))
		for _, b := range row.Buttons {
			buttons = append(buttons, discordgo.Button{
				Label:    b.Label,
				Style:    discordButtonStyle(b.Style),
				CustomID: b.CustomID,
				Disabled: b.Disabled,
			})
		}
		out = append(out, discordgo.ActionsRow{Components: buttons})
	}
	return out
}

func discordButtonStyle(s cmdadapter.ButtonStyle) discordgo.ButtonStyle {
	switch s {
	case cmdadapter.PrimaryButton:
		return discordgo.PrimaryButton
	case cmdadapter.SuccessButton:
		return discordgo.SuccessButton
	case cmdadapter.DangerButton:
		return discordgo.DangerButton
	default:
		return discordgo.SecondaryButton
	}
}

// DiscordSlashCommand renders a declaration into the form registration sends.
func DiscordSlashCommand(c *cmdadapter.SlashCommand) *discordgo.ApplicationCommand {
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

func discordCommandType(t cmdadapter.SlashCommandType) discordgo.ApplicationCommandType {
	switch t {
	case cmdadapter.MessageMenuCommand:
		return discordgo.MessageApplicationCommand
	case cmdadapter.UserMenuCommand:
		return discordgo.UserApplicationCommand
	default:
		return discordgo.ChatApplicationCommand
	}
}

func discordOptions(opts []cmdadapter.SlashOption) []*discordgo.ApplicationCommandOption {
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

func discordOptionType(t cmdadapter.SlashOptionType) discordgo.ApplicationCommandOptionType {
	switch t {
	case cmdadapter.OptionSubCommand:
		return discordgo.ApplicationCommandOptionSubCommand
	case cmdadapter.OptionSubCommandGroup:
		return discordgo.ApplicationCommandOptionSubCommandGroup
	case cmdadapter.OptionInteger:
		return discordgo.ApplicationCommandOptionInteger
	case cmdadapter.OptionBoolean:
		return discordgo.ApplicationCommandOptionBoolean
	default:
		return discordgo.ApplicationCommandOptionString
	}
}

func discordChoices(choices []cmdadapter.SlashChoice) []*discordgo.ApplicationCommandOptionChoice {
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

// SlashArguments reads the arguments off an interaction, which is the one
// direction that runs at invocation time rather than at registration.
func SlashArguments(opts []*discordgo.ApplicationCommandInteractionDataOption) []cmdadapter.SlashArgument {
	if len(opts) == 0 {
		return nil
	}
	out := make([]cmdadapter.SlashArgument, 0, len(opts))
	for _, o := range opts {
		if o == nil {
			continue
		}
		out = append(out, cmdadapter.SlashArgument{
			Name:    o.Name,
			Type:    slashOptionType(o.Type),
			Value:   o.Value,
			Options: SlashArguments(o.Options),
		})
	}
	return out
}

// slashOptionType is discordOptionType read backwards. Anything this package
// does not model reads as a string, which is what an unrecognised argument
// arrives as anyway.
func slashOptionType(t discordgo.ApplicationCommandOptionType) cmdadapter.SlashOptionType {
	switch t {
	case discordgo.ApplicationCommandOptionSubCommand:
		return cmdadapter.OptionSubCommand
	case discordgo.ApplicationCommandOptionSubCommandGroup:
		return cmdadapter.OptionSubCommandGroup
	case discordgo.ApplicationCommandOptionInteger:
		return cmdadapter.OptionInteger
	case discordgo.ApplicationCommandOptionBoolean:
		return cmdadapter.OptionBoolean
	default:
		return cmdadapter.OptionString
	}
}
