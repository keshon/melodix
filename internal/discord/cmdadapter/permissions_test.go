package cmdadapter

import (
	"errors"
	"testing"
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

// The three interaction contexts all reach the same helper, so all three have
// to benefit. Missing one means a context menu or a button click still fails
// where a slash command does not.
func TestEveryInteractionContextUsesTheCarriedPermissions(t *testing.T) {
	who := Invoker{
		GuildID: "g1", ChannelID: "c1", UserID: "u1",
		Permissions: 0x20, PermissionsKnown: true,
	}

	for name, ctx := range map[string]interface{ MemberPermissions() (int64, error) }{
		"slash":     &SlashInteractionContext{API: &coldCacheAPI{}, Invoker: who},
		"component": &ComponentInteractionContext{API: &coldCacheAPI{}, Invoker: who},
		"menu":      &MessageApplicationCommandContext{API: &coldCacheAPI{}, Invoker: who},
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
