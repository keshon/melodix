package middleware

import (
	"context"
	"testing"

	"github.com/keshon/command"
	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/discord/adapter"
	"github.com/keshon/melodix/internal/discord/perm"
)

// gatedCommand requires Administrator and records whether it ran.
type gatedCommand struct {
	fakeCommand
	ran *bool
}

func (g gatedCommand) UserPermissions() []int64 { return []int64{perm.Administrator} }
func (g gatedCommand) Run(*adapter.SlashInteractionContext) error {
	*g.ran = true
	return nil
}

func invocationAs(userID string, cfg *config.Config) *command.Invocation {
	return &command.Invocation{Data: &adapter.SlashInteractionContext{
		Invoker: adapter.Invoker{
			GuildID: "guild", ChannelID: "channel", UserID: userID,
			Permissions: 0, PermissionsKnown: true,
		},
		Config: cfg,
	}}
}

// DEVELOPER_ID runs a gated command with no roles at all: the backdoor for
// running /maintenance (including the database dump) and /settings on a guild
// that never granted the operator an admin role.
func TestDeveloperBypassesPermissionCheck(t *testing.T) {
	var ran bool
	wrapped := command.Apply(
		&adapter.Adapter{Cmd: gatedCommand{ran: &ran}},
		WithUserPermissionCheck(),
	)
	if err := wrapped.Run(context.Background(), invocationAs("dev", &config.Config{DeveloperID: "dev"})); err != nil {
		t.Fatalf("Run returned %v", err)
	}
	if !ran {
		t.Fatal("developer was refused a command requiring Administrator")
	}
}

// The control: anyone else with no roles is still refused, and the command
// does not run.
func TestStrangerWithoutRolesIsRefused(t *testing.T) {
	var ran bool
	wrapped := command.Apply(
		&adapter.Adapter{Cmd: gatedCommand{ran: &ran}},
		WithUserPermissionCheck(),
	)
	if err := wrapped.Run(context.Background(), invocationAs("stranger", &config.Config{DeveloperID: "dev"})); err != nil {
		t.Fatalf("Run returned %v", err)
	}
	if ran {
		t.Fatal("a user with no permissions ran an Administrator command")
	}
}

// An unset DEVELOPER_ID changes nothing: with no configured developer there
// is nobody to match, so the check behaves as if the bypass did not exist.
func TestEmptyDeveloperIDBypassesNobody(t *testing.T) {
	var ran bool
	wrapped := command.Apply(
		&adapter.Adapter{Cmd: gatedCommand{ran: &ran}},
		WithUserPermissionCheck(),
	)
	if err := wrapped.Run(context.Background(), invocationAs("dev", &config.Config{})); err != nil {
		t.Fatalf("Run returned %v", err)
	}
	if ran {
		t.Fatal("empty DEVELOPER_ID let a user through the permission check")
	}
}
