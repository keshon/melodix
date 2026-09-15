package adapter

import (
	"testing"
	"time"
)

// Some refusals are only worth saying quietly. A command group is usually
// disabled to keep it out of a channel, so a public notice every time somebody
// trips over it puts back the noise the admin was removing.
func TestCanReplyPrivatelyOnlyWhereItIsTrue(t *testing.T) {
	withResponder := &SlashInteractionContext{Responder: stubResponder{}}
	if !withResponder.CanReplyPrivately() {
		t.Error("slash interaction with a responder cannot reply privately")
	}

	withoutResponder := &SlashInteractionContext{}
	if withoutResponder.CanReplyPrivately() {
		t.Error("slash interaction with no responder claims a private reply")
	}
}

// A caller nobody could identify has no permissions to resolve, and asking
// anyway spends a request to be told so. The sentinel has to be recognised as
// well as the empty string, or an unknown caller is looked up by the literal
// name "unknown".
func TestUnknownCallersAreNotLookedUp(t *testing.T) {
	api := &countingAPI{}

	for _, who := range []Invoker{
		{UserID: "", ChannelID: "c"},
		{UserID: UnknownUserID, ChannelID: "c"},
	} {
		c := &SlashInteractionContext{Invoker: who, API: api}
		perms, err := c.MemberPermissions()
		if err != nil || perms != 0 {
			t.Errorf("caller %q: perms = %d, err = %v; want 0, nil", who.UserID, perms, err)
		}
	}
	if api.permissionCalls != 0 {
		t.Errorf("asked Discord about %d unidentifiable callers, want 0", api.permissionCalls)
	}

	known := &SlashInteractionContext{Invoker: Invoker{UserID: "111", ChannelID: "c"}, API: api}
	if _, err := known.MemberPermissions(); err != nil {
		t.Fatalf("known caller: %v", err)
	}
	if api.permissionCalls != 1 {
		t.Errorf("permission calls for a known caller = %d, want 1", api.permissionCalls)
	}
}

// Every context has to satisfy the interface, or a middleware silently stops
// applying to whatever was missed -- which for the permission check would mean
// a command running unchecked.
func TestEveryContextIsACommandContext(t *testing.T) {
	var (
		_ CommandContext = (*SlashInteractionContext)(nil)
		_ CommandContext = (*ComponentInteractionContext)(nil)
	)
}

// Every interaction context has to satisfy Interaction, or the shared playback
// helpers stop accepting one of the two ways a track gets queued.
func TestEveryInteractionContextIsAnInteraction(t *testing.T) {
	var (
		_ Interaction = (*SlashInteractionContext)(nil)
		_ Interaction = (*ComponentInteractionContext)(nil)
	)
}

type countingAPI struct {
	permissionCalls int
}

func (a *countingAPI) MemberPermissions(string, string) (int64, error) {
	a.permissionCalls++
	return 0, nil
}
func (*countingAPI) CheckBotPermissions(string) bool               { return false }
func (*countingAPI) CheckBotVoicePermissions(string) (bool, error) { return false, nil }
func (*countingAPI) SendChannelMessage(string, string) error       { return nil }
func (*countingAPI) SendChannelEmbed(string, *Embed) error         { return nil }
func (*countingAPI) GuildInfo(string) (GuildInfo, error)           { return GuildInfo{}, nil }
func (*countingAPI) Latency() time.Duration                        { return 0 }
func (*countingAPI) EmbedColor() int                               { return 0 }

type stubResponder struct{}

func (stubResponder) AckDeferred(bool) error { return nil }
func (stubResponder) Respond(Reply) error    { return nil }
func (stubResponder) Followup(Reply) error   { return nil }
func (stubResponder) AnswerEmbedMessage(*Embed) (string, string, error) {
	return "", "", nil
}
func (stubResponder) EditResponseText(string) error { return nil }
func (stubResponder) ReplaceMessage(*Embed) error   { return nil }
func (stubResponder) ResolveDeferred() error        { return nil }
