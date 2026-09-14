package reply

import (
	"testing"

	"github.com/bwmarrin/discordgo"

	"github.com/keshon/melodix/internal/discord/cmdadapter"
)

// Every option kind has to land on the value Discord means by it. Getting one
// wrong registers the command with the wrong sort of argument, which Discord
// accepts and users then cannot use -- there is no error to notice.
func TestOptionTypesMapToDiscords(t *testing.T) {
	cases := []struct {
		name string
		got  cmdadapter.SlashOptionType
		want discordgo.ApplicationCommandOptionType
	}{
		{"subcommand", cmdadapter.OptionSubCommand, discordgo.ApplicationCommandOptionSubCommand},
		{"subcommand group", cmdadapter.OptionSubCommandGroup, discordgo.ApplicationCommandOptionSubCommandGroup},
		{"string", cmdadapter.OptionString, discordgo.ApplicationCommandOptionString},
		{"integer", cmdadapter.OptionInteger, discordgo.ApplicationCommandOptionInteger},
		{"boolean", cmdadapter.OptionBoolean, discordgo.ApplicationCommandOptionBoolean},
	}

	for _, tc := range cases {
		if got := discordOptionType(tc.got); got != tc.want {
			t.Errorf("%s: mapped to %v, want %v", tc.name, got, tc.want)
		}
	}
}

// A declaration that says nothing about its type is a chat-input command.
// Every command in this repo relies on that, by not saying.
func TestCommandTypeDefaultsToChatInput(t *testing.T) {
	def := DiscordSlashCommand(&cmdadapter.SlashCommand{Name: "play"})

	if def.Type != discordgo.ChatApplicationCommand {
		t.Fatalf("type = %v, want a chat-input command", def.Type)
	}
}

// Subcommands nest, and a group nests one deeper. This is the shape /settings
// and /commands actually register, so the recursion has to survive both levels
// with its choices intact.
func TestNestedOptionsAndChoicesSurviveTranslation(t *testing.T) {
	minPage := 1.0
	def := DiscordSlashCommand(&cmdadapter.SlashCommand{
		Name:        "settings",
		Description: "server settings",
		Options: []cmdadapter.SlashOption{{
			Type: cmdadapter.OptionSubCommandGroup,
			Name: "commands",
			Options: []cmdadapter.SlashOption{{
				Type:        cmdadapter.OptionSubCommand,
				Name:        "enable",
				Description: "turn a group on",
				Options: []cmdadapter.SlashOption{{
					Type:     cmdadapter.OptionString,
					Name:     "group",
					Required: true,
					Choices:  []cmdadapter.SlashChoice{{Name: "Music", Value: "music"}},
				}, {
					Type:     cmdadapter.OptionInteger,
					Name:     "page",
					MinValue: &minPage,
					MaxValue: 10,
				}},
			}},
		}},
	})

	group := def.Options[0]
	if group.Type != discordgo.ApplicationCommandOptionSubCommandGroup {
		t.Fatalf("group type = %v", group.Type)
	}
	sub := group.Options[0]
	if sub.Type != discordgo.ApplicationCommandOptionSubCommand || sub.Name != "enable" {
		t.Fatalf("subcommand = %v %q", sub.Type, sub.Name)
	}

	choice := sub.Options[0]
	if !choice.Required || len(choice.Choices) != 1 || choice.Choices[0].Value != "music" {
		t.Fatalf("choices did not survive: %+v", choice)
	}

	page := sub.Options[1]
	if page.MinValue == nil || *page.MinValue != 1 || page.MaxValue != 10 {
		t.Fatalf("numeric bounds did not survive: %+v", page)
	}
}

// Nothing to declare stays nothing, rather than an empty slice Discord would
// have to interpret.
func TestEmptyDeclarationsStayEmpty(t *testing.T) {
	if DiscordSlashCommand(nil) != nil {
		t.Error("a nil declaration produced a command")
	}
	if def := DiscordSlashCommand(&cmdadapter.SlashCommand{Name: "stop"}); def.Options != nil {
		t.Errorf("a command with no options produced %v", def.Options)
	}
}
