package cmdadapter

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

// coldCacheAPI is the connection as it behaves for a member nobody has told it
// about, which for a guild past Discord's large-member threshold is most of
// them: GUILD_CREATE carries only some members, and the rest are learned from
// events a music bot has no other use for.
type coldCacheAPI struct {
	SessionAPI
	asked bool
}

func (a *coldCacheAPI) MemberPermissions(string, string) (int64, error) {
	a.asked = true
	return 0, errors.New("member 365177820663513089 not in cache")
}

// A permission check that depends on cache warmth is a command that works or
// does not depending on what else the bot happens to subscribe to. Discord
// states the caller's permissions on the interaction, so an invocation that
// carried them must never ask anything else.
func TestPermissionsComeFromTheInvocationNotTheCache(t *testing.T) {
	api := &coldCacheAPI{}
	ctx := &SlashInteractionContext{
		API: api,
		Invoker: Invoker{
			GuildID:          "g1",
			ChannelID:        "c1",
			UserID:           "u1",
			Permissions:      0x8, // Administrator
			PermissionsKnown: true,
		},
	}

	got, err := ctx.MemberPermissions()
	if err != nil {
		t.Fatalf("a command was refused over a cache it did not need: %v", err)
	}
	if got != 0x8 {
		t.Fatalf("permissions = %#x, want what the interaction said", got)
	}
	if api.asked {
		t.Fatal("the cache was consulted even though the invocation stated the answer")
	}
}

// Zero is a real answer, not a missing one: a member with no permissions in a
// channel must not fall through to a lookup that would report something else.
func TestNoPermissionsIsStillAnAnswer(t *testing.T) {
	api := &coldCacheAPI{}
	ctx := &SlashInteractionContext{
		API:     api,
		Invoker: Invoker{GuildID: "g1", ChannelID: "c1", UserID: "u1", PermissionsKnown: true},
	}

	got, err := ctx.MemberPermissions()
	if err != nil || got != 0 {
		t.Fatalf("MemberPermissions = (%#x, %v), want (0, nil)", got, err)
	}
	if api.asked {
		t.Fatal("a caller with no permissions was looked up anyway")
	}
}

// An invocation that carried no permissions -- anything that is not an
// interaction -- still has the lookup to fall back on.
func TestAnInvocationWithoutPermissionsStillAsks(t *testing.T) {
	api := &coldCacheAPI{}
	ctx := &SlashInteractionContext{
		API:     api,
		Invoker: Invoker{GuildID: "g1", ChannelID: "c1", UserID: "u1"},
	}

	if _, err := ctx.MemberPermissions(); err == nil {
		t.Fatal("want the lookup's error, got none")
	}
	if !api.asked {
		t.Fatal("nothing was asked and nothing was carried")
	}
}

// Both interaction contexts reach the same helper, so both have to benefit:
// missing one means a button click still fails where a slash command does
// not.
func TestBothInteractionContextsUseTheCarriedPermissions(t *testing.T) {
	who := Invoker{
		GuildID: "g1", ChannelID: "c1", UserID: "u1",
		Permissions: 0x20, PermissionsKnown: true,
	}

	for name, ctx := range map[string]interface{ MemberPermissions() (int64, error) }{
		"slash":     &SlashInteractionContext{API: &coldCacheAPI{}, Invoker: who},
		"component": &ComponentInteractionContext{API: &coldCacheAPI{}, Invoker: who},
	} {
		got, err := ctx.MemberPermissions()
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got != 0x20 {
			t.Errorf("%s: permissions = %#x, want 0x20", name, got)
		}
	}
}

// failingResponder is a reply that cannot be delivered, which is what every
// one of these looks like once the interaction it was answering has expired.
type failingResponder struct{ Responder }

func (failingResponder) Respond(Reply) error {
	return errors.New("40060: interaction has already been acknowledged")
}
func (failingResponder) AckDeferred(bool) error { return errors.New("10062: unknown interaction") }

// No command checks the error from a reply, and none reasonably could -- by
// the time one fails the interaction is gone. What must not happen is the
// pairing of a user who saw no answer with a log that says nothing happened,
// so the failure is reported here rather than at twenty-seven call sites or
// nowhere.
func TestAFailedReplyIsReportedEvenWhenNobodyChecksIt(t *testing.T) {
	var logged bytes.Buffer
	ctx := &SlashInteractionContext{
		Responder: failingResponder{},
		AppLog:    zerolog.New(&logged),
	}

	// Exactly how a command writes it: no error check.
	_ = ctx.Defer()
	_ = ctx.Respond(&Embed{Description: "hello"})
	_ = ctx.ReplyEphemeral("nope")

	out := logged.String()
	if strings.Count(out, "reply_failed") != 3 {
		t.Fatalf("want three reply_failed events, got:\n%s", out)
	}
	for _, kind := range []string{`"defer"`, `"respond"`, `"respond_ephemeral"`} {
		if !strings.Contains(out, kind) {
			t.Errorf("no report named %s:\n%s", kind, out)
		}
	}
}
