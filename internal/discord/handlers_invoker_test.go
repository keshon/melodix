package discord

import (
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/internal/discord/cmdadapter"
)

func interaction(guildID, channelID string, member *discordgo.Member, user *discordgo.User) *discordgo.InteractionCreate {
	return &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			GuildID:   guildID,
			ChannelID: channelID,
			Member:    member,
			User:      user,
		},
	}
}

// A guild interaction carries Member, a direct message carries User, and
// neither is guaranteed. Getting the order wrong means the audit log names the
// wrong person, or nobody.
//
// This used to be answered by the context, on demand, off the event it kept
// for the purpose. It is answered once here instead, which is what lets the
// contexts hold no event at all.
func TestInteractionInvokerResolvesTheCaller(t *testing.T) {
	member := &discordgo.Member{User: &discordgo.User{ID: "111", Username: "from-member"}}
	dmUser := &discordgo.User{ID: "222", Username: "from-user"}

	cases := []struct {
		name     string
		event    *discordgo.InteractionCreate
		wantID   string
		wantName string
	}{
		{"member wins", interaction("g", "c", member, dmUser), "111", "from-member"},
		{"user when there is no member", interaction("", "c", nil, dmUser), "222", "from-user"},
		{"sentinel when there is neither", interaction("g", "c", nil, nil),
			cmdadapter.UnknownUserID, cmdadapter.UnknownUsername},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			who := interactionInvoker(tc.event)
			if who.UserID != tc.wantID {
				t.Errorf("UserID = %q, want %q", who.UserID, tc.wantID)
			}
			if who.Username != tc.wantName {
				t.Errorf("Username = %q, want %q", who.Username, tc.wantName)
			}
		})
	}
}

// The guild and channel travel with the caller, and a direct message has no
// guild -- which is the whole basis of the guild-only middleware.
func TestInteractionInvokerCarriesTheLocation(t *testing.T) {
	who := interactionInvoker(interaction("g1", "c1", nil, nil))
	if who.GuildID != "g1" || who.ChannelID != "c1" {
		t.Errorf("location = %+v", who)
	}

	dm := interactionInvoker(interaction("", "c2", nil, nil))
	if dm.GuildID != "" {
		t.Errorf("a direct message reported guild %q", dm.GuildID)
	}
}

// A nil event must answer rather than panic: it reaches the sentinel the same
// way an interaction carrying nobody does.
func TestInteractionInvokerSurvivesANilEvent(t *testing.T) {
	who := interactionInvoker(nil)
	if who.UserID != cmdadapter.UnknownUserID || who.Username != cmdadapter.UnknownUsername {
		t.Errorf("nil event resolved to %+v", who)
	}
}

// A reaction does not always carry the member, and an audit row naming an ID
// is worth more than one naming nobody.
func TestReactionNameFallsBackToTheUserID(t *testing.T) {
	bare := &discordgo.MessageReactionAdd{
		MessageReaction: &discordgo.MessageReaction{UserID: "333"},
	}
	if got := reactorName(bare); got != "333" {
		t.Errorf("Username = %q, want the user ID", got)
	}

	named := &discordgo.MessageReactionAdd{
		MessageReaction: &discordgo.MessageReaction{UserID: "333"},
		Member:          &discordgo.Member{User: &discordgo.User{ID: "333", Username: "reactor"}},
	}
	if got := reactorName(named); got != "reactor" {
		t.Errorf("Username = %q, want the member's name", got)
	}
}

// A webhook message can arrive with no author at all, and the fallbacks match
// what an interaction reports so the audit log reads the same either way.
func TestMessageAuthorFallsBackToTheSentinel(t *testing.T) {
	if got := authorID(nil); got != cmdadapter.UnknownUserID {
		t.Errorf("authorID(nil) = %q", got)
	}
	if got := authorName(nil); got != cmdadapter.UnknownUsername {
		t.Errorf("authorName(nil) = %q", got)
	}
	u := &discordgo.User{ID: "444", Username: "poster"}
	if authorID(u) != "444" || authorName(u) != "poster" {
		t.Errorf("author = %q/%q", authorID(u), authorName(u))
	}
}
