// Package cli is the terminal frontend: a REPL over the same music service the
// bot uses, with commands that mirror the bot's wherever a terminal can.
package cli

import (
	"bufio"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/keshon/melodix/internal/music"
	"github.com/keshon/melodix/pkg/music/player"
)

// Command is one thing the REPL does. Category matches the bot's categories, so
// both frontends group their commands under the same headings.
type Command interface {
	Name() string
	Aliases() []string
	Description() string
	Category() string
	// Usage is the argument synopsis after the name, e.g. "<query> [source=…]".
	Usage() string
	Run(c *Context, args []string) error
}

// Context is what a running command reaches: the service, the terminal's
// player scope, and the terminal itself.
type Context struct {
	Music    *music.Service
	Scope    string
	Registry *Registry

	out io.Writer
	in  *bufio.Scanner
}

// Player is the terminal's player.
func (c *Context) Player() *player.Player { return c.Music.Player(c.Scope) }

// Println writes a line to the terminal.
func (c *Context) Println(a ...any) { _, _ = fmt.Fprintln(c.out, a...) }

// Printf writes formatted text to the terminal.
func (c *Context) Printf(format string, a ...any) { _, _ = fmt.Fprintf(c.out, format, a...) }

// Ask prints prompt and reads one line of answer. ok is false when the input
// has ended.
func (c *Context) Ask(prompt string) (answer string, ok bool) {
	c.Printf("%s", prompt)
	if !c.in.Scan() {
		return "", false
	}
	return strings.TrimSpace(c.in.Text()), true
}

// Registry holds the commands the REPL knows, in registration order.
type Registry struct {
	commands []Command
	byName   map[string]Command
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{byName: make(map[string]Command)}
}

// Register adds c under its name and aliases. A name registered twice is a
// programming error, so it panics rather than letting one command shadow
// another.
func (r *Registry) Register(c Command) {
	for _, name := range append([]string{c.Name()}, c.Aliases()...) {
		if _, taken := r.byName[name]; taken || isQuit(name) {
			panic(fmt.Sprintf("cli: command name %q is already taken", name))
		}
		r.byName[name] = c
	}
	r.commands = append(r.commands, c)
}

// All returns every command, in registration order.
func (r *Registry) All() []Command { return append([]Command(nil), r.commands...) }

// Lookup finds a command by name or alias.
func (r *Registry) Lookup(name string) (Command, bool) {
	c, ok := r.byName[name]
	return c, ok
}

// quitWords end the REPL. They are the loop's own rather than a command, the
// way closing a terminal is not something the music service does.
var quitWords = []string{"quit", "exit", "q"}

func isQuit(word string) bool { return slices.Contains(quitWords, word) }

// Run reads commands from in until it ends or someone quits, writing to out.
// A failing command is reported and the loop carries on.
func Run(reg *Registry, svc *music.Service, scope string, in io.Reader, out io.Writer) error {
	c := &Context{Music: svc, Scope: scope, Registry: reg, out: out, in: bufio.NewScanner(in)}
	c.Println("Type help for commands, quit to leave.")
	for {
		line, ok := c.Ask("> ")
		if !ok {
			return c.in.Err()
		}
		words := SplitQuoted(line)
		if len(words) == 0 {
			continue
		}
		name, args := words[0], words[1:]
		if isQuit(name) {
			return nil
		}
		cmd, ok := reg.Lookup(name)
		if !ok {
			c.Printf("Unknown command %q. Type help for the list.\n", name)
			continue
		}
		if err := cmd.Run(c, args); err != nil {
			c.Println("Error:", err)
		}
	}
}

// SplitQuoted splits a line on spaces but keeps quoted segments as one word.
func SplitQuoted(s string) []string {
	var out []string
	var buf strings.Builder
	inQuote := false
	for _, r := range s {
		switch {
		case r == '"' || r == '\'':
			inQuote = !inQuote
		case (r == ' ' || r == '\t') && !inQuote:
			if buf.Len() > 0 {
				out = append(out, buf.String())
				buf.Reset()
			}
		default:
			buf.WriteRune(r)
		}
	}
	if buf.Len() > 0 {
		out = append(out, buf.String())
	}
	return out
}

// Options separates key=value words, for the given keys only, from the rest.
// Anything else stays a word, so a URL's own query string is left alone.
func Options(args []string, keys ...string) (words []string, opts map[string]string) {
	opts = make(map[string]string)
	for _, a := range args {
		k, v, found := strings.Cut(a, "=")
		if found && slices.Contains(keys, k) {
			opts[k] = v
			continue
		}
		words = append(words, a)
	}
	return words, opts
}
