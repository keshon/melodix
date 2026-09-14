package cmdadapter

import "github.com/bwmarrin/discordgo"

// Button is a control under a message. CustomID comes back when it is pressed,
// and is the only thing that does -- a chooser that puts everything it needs
// into the id needs no server-side memory of what it offered, so it survives a
// restart and two people searching at once cannot interfere.
type Button struct {
	Label    string
	Style    ButtonStyle
	CustomID string
	Disabled bool
}

// ButtonStyle is how a button is drawn. The zero value is the neutral grey,
// because a row of choices is the common case here and none of them is the
// dangerous one or the recommended one.
type ButtonStyle int

const (
	SecondaryButton ButtonStyle = iota
	PrimaryButton
	SuccessButton
	DangerButton
)

// ActionRow is a row of controls. Discord allows five buttons per row and five
// rows per message; nothing here is close, so the limits are not enforced --
// Discord rejects the message if they are ever exceeded, which is a clearer
// error than one invented here.
type ActionRow struct {
	Buttons []Button
}

// DiscordComponents renders rows into the wire format. Empty stays empty:
// sending an empty component list is how a chooser is consumed, and that is
// different from sending none at all.
func DiscordComponents(rows []ActionRow) []discordgo.MessageComponent {
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

func discordButtonStyle(s ButtonStyle) discordgo.ButtonStyle {
	switch s {
	case PrimaryButton:
		return discordgo.PrimaryButton
	case SuccessButton:
		return discordgo.SuccessButton
	case DangerButton:
		return discordgo.DangerButton
	default:
		return discordgo.SecondaryButton
	}
}
