package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/keshon/command"
	"github.com/rs/zerolog"

	"github.com/keshon/melodix/internal/discord/adapter"
)

// slashRef matches a command path quoted in user-facing text, like
// "`/settings commands status`": a slash, then words, stopping at anything
// that is not a lowercase word (an argument such as <id>, a closing backtick).
var slashRef = regexp.MustCompile("`/([a-z][a-z-]*(?: [a-z][a-z-]*)*)")

// A reply that sends people to a command must name one that exists. Nothing
// else notices when it does not: the string compiles, the test for the command
// it lives in stays green, and the user types a path Discord has never heard
// of. The disabled-group refusal told users to run `/commands status` for as
// long as the group lived under /settings.
//
// Only string literals are read. Comments may mention commands from other
// projects, and do.
func TestRepliesNameCommandsThatExist(t *testing.T) {
	registerCommands(nil, zerolog.Nop())

	valid := map[string]bool{}
	for _, c := range command.DefaultRegistry.GetAll() {
		sp, ok := command.Root(c).(adapter.SlashProvider)
		if !ok || sp.SlashDefinition() == nil {
			continue
		}
		def := sp.SlashDefinition()
		valid[def.Name] = true
		addPaths(valid, def.Name, def.Options)
	}
	if len(valid) == 0 {
		t.Fatal("no slash definitions registered; the check would pass vacuously")
	}

	root := filepath.Join("..", "..")
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, src, 0)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				text, err := strconv.Unquote(lit.Value)
				if err != nil {
					return true
				}
				for _, m := range slashRef.FindAllStringSubmatch(text, -1) {
					if !resolves(valid, m[1]) {
						t.Errorf("%s: names `/%s`, which is not a registered command",
							fset.Position(lit.Pos()), m[1])
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func addPaths(valid map[string]bool, prefix string, options []adapter.SlashOption) {
	for _, opt := range options {
		if opt.Type != adapter.OptionSubCommand && opt.Type != adapter.OptionSubCommandGroup {
			continue
		}
		path := prefix + " " + opt.Name
		valid[path] = true
		addPaths(valid, path, opt.Options)
	}
}

// resolves reports whether ref starts with a registered path: `/play something`
// is the play command followed by its input, not a subcommand called something.
func resolves(valid map[string]bool, ref string) bool {
	words := strings.Fields(ref)
	if !valid[words[0]] {
		return false
	}
	path := words[0]
	for _, w := range words[1:] {
		next := path + " " + w
		if !valid[next] {
			return !hasSubcommands(valid, path)
		}
		path = next
	}
	return true
}

func hasSubcommands(valid map[string]bool, path string) bool {
	for p := range valid {
		if strings.HasPrefix(p, path+" ") {
			return true
		}
	}
	return false
}
