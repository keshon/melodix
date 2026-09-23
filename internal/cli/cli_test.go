package cli

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/music"
	"github.com/rs/zerolog"
)

type echo struct{ got [][]string }

func (*echo) Name() string        { return "echo" }
func (*echo) Aliases() []string   { return []string{"e"} }
func (*echo) Description() string { return "say it back" }
func (*echo) Category() string    { return "ℹ️ Information" }
func (*echo) Usage() string       { return "<words>" }
func (e *echo) Run(c *Context, args []string) error {
	e.got = append(e.got, args)
	if len(args) > 0 && args[0] == "fail" {
		return errors.New("asked to fail")
	}
	answer, _ := c.Ask("again? ")
	c.Println("heard", strings.Join(args, "+"), "then", answer)
	return nil
}

func run(t *testing.T, reg *Registry, input string) string {
	t.Helper()
	svc := music.New(&config.Config{}, nil, zerolog.Nop(), music.Hooks{})
	var out strings.Builder
	if err := Run(reg, svc, "cli", strings.NewReader(input), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return out.String()
}

func TestRunDispatchesByNameAndAlias(t *testing.T) {
	e := &echo{}
	reg := NewRegistry()
	reg.Register(e)

	out := run(t, reg, "echo \"two words\" x\nyes\ne fail\nnope\nquit\necho never\n")

	if want := [][]string{{"two words", "x"}, {"fail"}}; !slices.EqualFunc(e.got, want, slices.Equal) {
		t.Fatalf("runs = %q, want %q", e.got, want)
	}
	for _, want := range []string{"heard two words+x then yes", "Error: asked to fail", `Unknown command "nope"`} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestRunEndsWithInput(t *testing.T) {
	if out := run(t, NewRegistry(), ""); !strings.Contains(out, "Type help") {
		t.Fatalf("output = %q", out)
	}
}

func TestRegisterRefusesTakenNames(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&echo{})
	for name, c := range map[string]Command{"duplicate": &echo{}, "quit word": &quitter{}} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("a taken name was registered")
				}
			}()
			reg.Register(c)
		})
	}
}

type quitter struct{ echo }

func (*quitter) Name() string      { return "fresh" }
func (*quitter) Aliases() []string { return []string{"q"} }

func TestOptionsKeepsURLQueries(t *testing.T) {
	words, opts := Options([]string{"https://youtu.be/x?t=1", "lo-fi", "source=soundcloud", "mood=calm"}, "source")
	if want := []string{"https://youtu.be/x?t=1", "lo-fi", "mood=calm"}; !slices.Equal(words, want) {
		t.Fatalf("words = %q, want %q", words, want)
	}
	if opts["source"] != "soundcloud" || len(opts) != 1 {
		t.Fatalf("opts = %v", opts)
	}
}

func TestSplitQuoted(t *testing.T) {
	got := SplitQuoted(`play "never gonna" 'give up'  now`)
	if want := []string{"play", "never gonna", "give up", "now"}; !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}
