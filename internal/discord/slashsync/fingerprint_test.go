package slashsync

import (
	"encoding/json"
	"testing"

	"github.com/disgoorg/disgo/discord"

	"github.com/keshon/melodix/internal/discord/adapter"
	"github.com/keshon/melodix/internal/discord/reply"
)

func minValue(f float64) *float64 { return &f }

func settingsCommand() *adapter.SlashCommand {
	return &adapter.SlashCommand{
		Name:        "settings",
		Description: "bot settings",
		Options: []adapter.SlashOption{{
			Type: adapter.OptionSubCommandGroup, Name: "commands", Description: "command groups",
			Options: []adapter.SlashOption{{
				Type: adapter.OptionSubCommand, Name: "enable", Description: "enable a group",
				Options: []adapter.SlashOption{{
					Type: adapter.OptionString, Name: "group", Description: "group name",
					Required: true,
					Choices: []adapter.SlashChoice{
						{Name: "music", Value: "music"},
						{Name: "core", Value: "core"},
					},
				}},
			}},
		}},
	}
}

func historyCommand() *adapter.SlashCommand {
	return &adapter.SlashCommand{
		Name:        "history",
		Description: "played tracks",
		Options: []adapter.SlashOption{
			{
				Type: adapter.OptionInteger, Name: "page", Description: "page",
				MinValue: minValue(1), MaxValue: 100,
			},
			{Type: adapter.OptionBoolean, Name: "all", Description: "every guild"},
		},
	}
}

// roundTrip renders a declaration the way registration does, then reads it
// back the way Discord reports it. It stands in for the API without one: the
// bytes are the same bytes.
func roundTrip(t *testing.T, decl *adapter.SlashCommand) *adapter.SlashCommand {
	t.Helper()

	raw, err := json.Marshal(reply.SlashCommandCreate(decl))
	if err != nil {
		t.Fatalf("marshalling the create form: %v", err)
	}
	var fetched discord.UnmarshalApplicationCommand
	if err := json.Unmarshal(raw, &fetched); err != nil {
		t.Fatalf("unmarshalling as a fetched command: %v", err)
	}
	return fromWire(fetched.ApplicationCommand)
}

// The sync compares what a command declares against what Discord reports. If
// those two fingerprint differently for an unchanged command, every startup
// re-edits every command -- N writes per guild on a link that is already
// throttled, forever, with nothing in the log to say why.
//
// This is the property that makes the comparison worth doing at all, and it
// cannot be checked by inspecting either side alone.
func TestAnUnchangedCommandFingerprintsTheSameAfterARoundTrip(t *testing.T) {
	for _, decl := range []*adapter.SlashCommand{
		settingsCommand(),
		historyCommand(),
		{Name: "ping", Description: "latency"},
		{Type: adapter.MessageMenuCommand, Name: "Add to queue"},
	} {
		t.Run(decl.Name, func(t *testing.T) {
			got := fingerprint(roundTrip(t, decl))
			want := fingerprint(decl)
			if got != want {
				t.Errorf("round trip changed the fingerprint\n declared: %s\n reported: %s\n"+
					"an unchanged command would be re-edited on every sync", want, got)
			}
		})
	}
}

// A real change has to be seen, or an edited command never reaches Discord and
// the difference is invisible until somebody tries to use it.
func TestRealChangesAreSeen(t *testing.T) {
	base := settingsCommand()

	renamed := settingsCommand()
	renamed.Name = "config"

	redescribed := settingsCommand()
	redescribed.Description = "something else"

	retyped := settingsCommand()
	retyped.Type = adapter.MessageMenuCommand

	argAdded := settingsCommand()
	argAdded.Options[0].Options[0].Options = append(argAdded.Options[0].Options[0].Options,
		adapter.SlashOption{Type: adapter.OptionBoolean, Name: "force", Description: "force"})

	choiceChanged := settingsCommand()
	choiceChanged.Options[0].Options[0].Options[0].Choices[0].Value = "muzak"

	requiredFlipped := settingsCommand()
	requiredFlipped.Options[0].Options[0].Options[0].Required = false

	for name, changed := range map[string]*adapter.SlashCommand{
		"name":         renamed,
		"description":  redescribed,
		"type":         retyped,
		"new argument": argAdded,
		"choice value": choiceChanged,
		"required":     requiredFlipped,
	} {
		t.Run(name, func(t *testing.T) {
			if fingerprint(changed) == fingerprint(base) {
				t.Errorf("a changed %s fingerprinted the same; the edit would never be sent", name)
			}
		})
	}
}

// Reordering a declaration in source is not a change to the command, and must
// not cost a write on every startup for as long as the order differs.
func TestReorderingOptionsIsNotAChange(t *testing.T) {
	a := historyCommand()
	b := historyCommand()
	b.Options[0], b.Options[1] = b.Options[1], b.Options[0]

	if fingerprint(a) != fingerprint(b) {
		t.Error("reordered options fingerprinted differently; every sync would edit")
	}
}

// Two different commands must not collide, or one of them is never updated.
func TestDifferentCommandsFingerprintDifferently(t *testing.T) {
	if fingerprint(settingsCommand()) == fingerprint(historyCommand()) {
		t.Error("two unrelated commands share a fingerprint")
	}
	if fingerprint(nil) != "" {
		t.Error("a nil declaration produced a fingerprint")
	}
}
