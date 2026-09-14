package cmdsync

import (
	"testing"

	"github.com/keshon/command"
	"github.com/keshon/melodix/internal/discord/cmdadapter"
)

// A command as the registry actually holds one: wrapped by cmdadapter.Adapter,
// then by whatever middleware was applied.
type declaringCommand struct{}

func (declaringCommand) Name() string              { return "probe" }
func (declaringCommand) Description() string       { return "a command that declares a slash form" }
func (declaringCommand) Group() string             { return "core" }
func (declaringCommand) Category() string          { return "core" }
func (declaringCommand) UserPermissions() []int64  { return nil }
func (declaringCommand) Run(ctx interface{}) error { return nil }
func (declaringCommand) SlashDefinition() *cmdadapter.SlashCommand {
	return &cmdadapter.SlashCommand{Name: "probe", Description: "probe"}
}

type menuCommand struct{ declaringCommand }

func (menuCommand) SlashDefinition() *cmdadapter.SlashCommand { return nil }
func (menuCommand) ContextDefinition() *cmdadapter.SlashCommand {
	return &cmdadapter.SlashCommand{Name: "Probe Message"}
}

// TestRegisteredCommandsResolveToADefinition is the check whose absence let a
// registration outage sit on a branch that was otherwise green.
//
// toApplicationCommand reaches the command through command.Root and a type
// assertion. An assertion does not fail to compile when the interface and the
// implementation drift apart — it just stops matching, every command resolves
// to no definition, and SyncGuildCommands then computes an empty desired set.
// An empty desired set does not mean "leave it alone": the reconcile deletes
// everything Discord still has, so the failure mode of this drift is a guild
// losing all of its slash commands.
func TestRegisteredCommandsResolveToADefinition(t *testing.T) {
	for _, tc := range []struct {
		name string
		cmd  cmdadapter.Handler
		want string
	}{
		{"slash", declaringCommand{}, "probe"},
		{"context menu", menuCommand{}, "Probe Message"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Apply with no middleware still wraps, which is what the registry
			// holds; Root has to unwrap back to the Adapter for this to work.
			c := command.Apply(&cmdadapter.Adapter{Cmd: tc.cmd})

			def := toApplicationCommand(c)
			if def == nil {
				t.Fatalf("a registered command resolved to no definition; "+
					"SyncGuildCommands would send an empty desired set and "+
					"delete the guild's commands (root is %T)", command.Root(c))
			}
			if def.Name != tc.want {
				t.Errorf("definition name = %q, want %q", def.Name, tc.want)
			}
		})
	}
}

// TestBuildCommandDefinitionsSeesTheRegistry guards the step between: a
// registry with commands in it must not produce an empty desired set.
func TestBuildCommandDefinitionsSeesTheRegistry(t *testing.T) {
	registry := command.NewRegistry()
	registry.Register(command.Apply(&cmdadapter.Adapter{Cmd: declaringCommand{}}))

	m := &Syncer{registry: registry}
	defs := m.buildCommandDefinitions()
	if len(defs) == 0 {
		t.Fatal("a registry holding one declaring command produced no " +
			"definitions to register")
	}
}
