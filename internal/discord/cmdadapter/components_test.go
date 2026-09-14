package cmdadapter

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

// The chooser puts everything it needs into the button id, so the id is the
// one field that must survive intact -- lose it and a click means nothing.
func TestButtonsKeepTheirCustomID(t *testing.T) {
	rows := DiscordComponents([]ActionRow{{Buttons: []Button{
		{Label: "1", CustomID: "yt:abc123"},
		{Label: "2", CustomID: "sc:def456", Disabled: true},
	}}})

	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	row, ok := rows[0].(discordgo.ActionsRow)
	if !ok {
		t.Fatalf("row is %T, want discordgo.ActionsRow", rows[0])
	}
	first := row.Components[0].(discordgo.Button)
	if first.CustomID != "yt:abc123" || first.Label != "1" {
		t.Errorf("first button = %+v", first)
	}
	if second := row.Components[1].(discordgo.Button); !second.Disabled {
		t.Errorf("second button lost its disabled state: %+v", second)
	}
}

// The zero value is the neutral grey, which is what a row of equal choices
// should look like. Every button the search chooser builds relies on it.
func TestButtonStyleDefaultsToSecondary(t *testing.T) {
	cases := map[ButtonStyle]discordgo.ButtonStyle{
		SecondaryButton: discordgo.SecondaryButton,
		PrimaryButton:   discordgo.PrimaryButton,
		SuccessButton:   discordgo.SuccessButton,
		DangerButton:    discordgo.DangerButton,
	}
	for got, want := range cases {
		if s := discordButtonStyle(got); s != want {
			t.Errorf("style %d mapped to %v, want %v", got, s, want)
		}
	}
}

// Sending an empty component list is how a chooser is consumed -- the buttons
// go away with the click that acts on them -- and that is a different message
// from one that never had any.
func TestNoRowsRendersAnEmptyListRatherThanNil(t *testing.T) {
	rows := DiscordComponents(nil)

	if rows == nil {
		t.Fatal("nil rows rendered as nil; consuming a chooser needs an empty list")
	}
	if len(rows) != 0 {
		t.Fatalf("got %d rows, want none", len(rows))
	}
}
