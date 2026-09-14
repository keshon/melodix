package cmdadapter

import (
	"io"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
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
func TestInteractionContextResolvesTheCaller(t *testing.T) {
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
		{"sentinel when there is neither", interaction("g", "c", nil, nil), UnknownUserID, "Unknown"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &SlashInteractionContext{Event: tc.event}
			if got := c.UserID(); got != tc.wantID {
				t.Errorf("UserID = %q, want %q", got, tc.wantID)
			}
			if got := c.Username(); got != tc.wantName {
				t.Errorf("Username = %q, want %q", got, tc.wantName)
			}
		})
	}
}

// Message commands are deliberately kept out of the audit log. Expressing that
// as "no logger to reach" rather than a branch in the middleware means there
// is no code path that could later be written to log them by accident.
func TestMessageContextIsNotAudited(t *testing.T) {
	c := &MessageContext{Event: &discordgo.MessageCreate{Message: &discordgo.Message{}}}

	if c.AuditLogger() != nil {
		t.Fatal("a message command reached an audit logger")
	}
}

// Some refusals are only worth saying quietly. A command group is usually
// disabled to keep it out of a channel, so a public notice every time somebody
// trips over it puts back the noise the admin was removing.
func TestCanReplyPrivatelyOnlyWhereItIsTrue(t *testing.T) {
	withResponder := &SlashInteractionContext{Event: interaction("g", "c", nil, nil), Responder: stubResponder{}}
	if !withResponder.CanReplyPrivately() {
		t.Error("slash interaction with a responder cannot reply privately")
	}

	withoutResponder := &SlashInteractionContext{Event: interaction("g", "c", nil, nil)}
	if withoutResponder.CanReplyPrivately() {
		t.Error("slash interaction with no responder claims a private reply")
	}

	message := &MessageContext{Event: &discordgo.MessageCreate{Message: &discordgo.Message{}}}
	if message.CanReplyPrivately() {
		t.Error("a plain message claims a private reply; Discord offers none")
	}
}

// A reaction does not always carry the member, and an audit row naming an ID
// is worth more than one naming nobody.
func TestReactionContextFallsBackToTheUserID(t *testing.T) {
	bare := &MessageReactionContext{Event: &discordgo.MessageReactionAdd{
		MessageReaction: &discordgo.MessageReaction{UserID: "333"},
	}}
	if got := bare.Username(); got != "333" {
		t.Errorf("Username = %q, want the user ID", got)
	}

	named := &MessageReactionContext{Event: &discordgo.MessageReactionAdd{
		MessageReaction: &discordgo.MessageReaction{UserID: "333"},
		Member:          &discordgo.Member{User: &discordgo.User{ID: "333", Username: "reactor"}},
	}}
	if got := named.Username(); got != "reactor" {
		t.Errorf("Username = %q, want the member's name", got)
	}
}

// Every context has to satisfy the interface, or a middleware silently stops
// applying to whatever was missed -- which for the permission check would mean
// a command running unchecked.
func TestEveryContextIsACommandContext(t *testing.T) {
	var (
		_ CommandContext = (*SlashInteractionContext)(nil)
		_ CommandContext = (*ComponentInteractionContext)(nil)
		_ CommandContext = (*MessageApplicationCommandContext)(nil)
		_ CommandContext = (*MessageContext)(nil)
		_ CommandContext = (*MessageReactionContext)(nil)
	)
}

type stubResponder struct{}

func (stubResponder) RespondEmbedEphemeral(*discordgo.Session, *discordgo.InteractionCreate, *Embed) error {
	return nil
}
func (stubResponder) RespondEmbed(*discordgo.Session, *discordgo.InteractionCreate, *Embed) error {
	return nil
}
func (stubResponder) CheckBotPermissions(*discordgo.Session, string) bool { return false }
func (stubResponder) CheckBotVoicePermissions(*discordgo.Session, string) (bool, error) {
	return false, nil
}
func (stubResponder) EmbedColor() int { return 0 }
func (stubResponder) AckDeferred(*discordgo.Session, *discordgo.InteractionCreate) error {
	return nil
}
func (stubResponder) AckDeferredEphemeral(*discordgo.Session, *discordgo.InteractionCreate) error {
	return nil
}
func (stubResponder) FollowupEmbed(*discordgo.Session, *discordgo.InteractionCreate, *Embed) error {
	return nil
}
func (stubResponder) FollowupEmbedEphemeral(*discordgo.Session, *discordgo.InteractionCreate, *Embed) error {
	return nil
}
func (stubResponder) EditResponse(*discordgo.Session, *discordgo.InteractionCreate, string) error {
	return nil
}
func (stubResponder) FollowupEmbedEphemeralWithComponents(*discordgo.Session, *discordgo.InteractionCreate, *Embed, []ActionRow) error {
	return nil
}
func (stubResponder) ReplaceComponentMessage(*discordgo.Session, *discordgo.InteractionCreate, *Embed) error {
	return nil
}
func (stubResponder) FollowupEmbedMessage(*discordgo.Session, *discordgo.InteractionCreate, *Embed) (string, string, error) {
	return "", "", nil
}
func (stubResponder) RespondEmbedEphemeralWithFile(*discordgo.Session, *discordgo.InteractionCreate, *Embed, io.Reader, string) error {
	return nil
}
func (stubResponder) RespondEphemeralText(*discordgo.Session, *discordgo.InteractionCreate, string) error {
	return nil
}
func (stubResponder) Latency(*discordgo.Session) time.Duration { return 0 }
func (stubResponder) GuildInfo(*discordgo.Session, string) (GuildInfo, error) {
	return GuildInfo{}, nil
}
