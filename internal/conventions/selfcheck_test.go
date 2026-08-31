package conventions

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// These test the checker itself, against inputs whose answer is known.
//
// Without them the whole arrangement has a blind spot: a scanner that returns
// nothing looks exactly like a codebase with nothing wrong. Break the regex in
// scanLogEvents and every rule still reports "0 violations, baseline 0", the
// build stays green, and the enforcement quietly stops — which is the same
// failure as an unenforced rule, wearing a passing test as a disguise.
//
// So each scanner is fed a fixture that must produce a violation and one that
// must not. A scanner that silently stops working now fails here instead of
// going unnoticed until someone counts by hand. This is where the regress
// stops: these assert fixed answers about literal inputs, so they cannot pass
// by finding nothing.

// fileOf builds a goFile from source text, so a fixture reads as the code it
// represents rather than as a slice of strings.
func fileOf(path, pkg, src string) goFile {
	return goFile{path: path, pkg: pkg, lines: strings.Split(src, "\n")}
}

// words returns a comment body of n four-character words, so a line can be made
// a precise length without tripping the unbreakable-token exemption.
func words(n int) string {
	return strings.TrimSpace(strings.Repeat("word ", n))
}

func TestScanCommentWidthSelfCheck(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"short comment", "// " + words(4), 0},
		{"exactly at the limit", "// " + words(15) + "xxx", 0},
		{"one column over", "// " + words(16), 1},
		{"indented comment counts its tabs", "\t\t// " + words(16), 1},
		{"go directive is exempt", "//go:generate " + words(16), 0},
		{"nolint directive is exempt", "//nolint:errcheck // " + words(16), 0},
		{"unbreakable token is exempt", "// see " + strings.Repeat("x", 90), 0},
		{
			"an unbreakable token does not license prose after it",
			"// see " + strings.Repeat("x", 40) + " " + words(16),
			1,
		},
		{"long code line is not a comment", strings.Repeat("x ", 60), 0},
		{"two bad lines count twice", "// " + words(16) + "\n// " + words(16), 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := scanCommentWidth(t, []goFile{fileOf("pkg/music/x.go", "x", c.src)})
			if len(got) != c.want {
				t.Errorf("got %d violations, want %d\n  source: %q", len(got), c.want, c.src)
			}
		})
	}

	// The limit must actually be the documented one, not merely "some number".
	over := "// " + words(1) + strings.Repeat(" ab", (maxCommentCols-6)/3+2)
	if len(over) <= maxCommentCols {
		t.Fatalf("fixture is %d cols, needs to exceed %d", len(over), maxCommentCols)
	}
	if got := scanCommentWidth(t, []goFile{fileOf("a.go", "a", over)}); len(got) != 1 {
		t.Errorf("a %d-column comment was not flagged at a %d-column limit", len(over), maxCommentCols)
	}
}

// logCall assembles a zerolog call as source text. The fixtures are built
// rather than written out because scanLogEvents reads source lines and cannot
// tell a fixture from the real thing: spelled literally, the bad cases below
// would be counted as genuine violations of this very file.
func logCall(prefix, method, event string) string {
	return "l.Info()." + prefix + method + `("` + event + `")`
}

func TestScanLogEventsSelfCheck(t *testing.T) {
	cases := []struct {
		name           string
		prefix, method string
		event          string
		want           int
	}{
		{"snake_case event", "", "Msg", "stream_opened", 0},
		{"digits are fine", "", "Msg", "op4_race_avoided", 0},
		{"single word", "", "Msg", "done", 0},
		{"spaces and capitals", "", "Msg", "Stream Opened", 1},
		{"kebab-case", "", "Msg", "stream-opened", 1},
		{"trailing underscore", "", "Msg", "opened_", 1},
		{"Msgf interpolates", "", "Msgf", "opened %s", 1},
		{"fields are untouched", `Str("a-B c", v).`, "Msg", "ok_now", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := logCall(c.prefix, c.method, c.event)
			got := scanLogEvents(t, []goFile{fileOf("internal/x.go", "x", src)})
			if len(got) != c.want {
				t.Errorf("got %d violations, want %d\n  source: %s", len(got), c.want, src)
			}
		})
	}

	// An event name computed at runtime is invisible to the literal scan, which
	// is how a live one sat undetected in the discordgo log bridge. Assembled
	// rather than written, for the reason logCall exists.
	runtimeName := `ev.Str("raw", raw).` + "Msg" + `(raw)`
	got := scanLogEvents(t, []goFile{fileOf("internal/x.go", "x", runtimeName)})
	if len(got) != 1 {
		t.Errorf("a non-literal event name gave %d violations, want 1: %s", len(got), runtimeName)
	}
	if len(got) == 1 && !strings.Contains(got[0].detail, "non-literal") {
		t.Errorf("detail should name the cause, got %q", got[0].detail)
	}
}

func TestScanErrorPrefixSelfCheck(t *testing.T) {
	const lib = "pkg/music/foo/foo.go"
	cases := []struct {
		name string
		path string
		src  string
		want int
	}{
		{"prefixed", lib, `return errors.New("foo: bad thing")`, 0},
		{"wrapped and prefixed", lib, `return fmt.Errorf("foo: open: %w", err)`, 0},
		{"runtime prefix", lib, `return fmt.Errorf("%s: open: %w", who, err)`, 0},
		{"unprefixed", lib, `return errors.New("bad thing happened")`, 1},
		{"wrong package", lib, `return errors.New("bar: bad thing")`, 1},
		{"exported sentinel is exempt", lib, `var ErrBad = errors.New("bad thing happened")`, 0},
		{"unexported sentinel is not", lib, `var errBad = errors.New("bad thing happened")`, 1},
		{"outside the library", "internal/x/x.go", `errors.New("bad thing happened")`, 0},
		{"test files are skipped", "pkg/music/foo/foo_test.go", `errors.New("bad thing")`, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := scanErrorPrefix(t, []goFile{fileOf(c.path, "foo", c.src)})
			if len(got) != c.want {
				t.Errorf("got %d violations, want %d\n  %s: %s", len(got), c.want, c.path, c.src)
			}
		})
	}
}

func TestCompareToBaselineSelfCheck(t *testing.T) {
	cases := []struct {
		name          string
		allowed       map[string]int
		counts        map[string]int
		worse, better []string
	}{
		{"unchanged", map[string]int{"a.go": 3}, map[string]int{"a.go": 3}, nil, nil},
		{"got worse", map[string]int{"a.go": 3}, map[string]int{"a.go": 4}, []string{"a.go"}, nil},
		{"got better", map[string]int{"a.go": 3}, map[string]int{"a.go": 1}, nil, []string{"a.go"}},
		{"fixed entirely", map[string]int{"a.go": 3}, map[string]int{}, nil, []string{"a.go"}},
		{"a new file owes nothing", map[string]int{}, map[string]int{"new.go": 1}, []string{"new.go"}, nil},
		{"clean new file", map[string]int{}, map[string]int{}, nil, nil},
		{
			"one improves while another rots",
			map[string]int{"a.go": 3, "b.go": 2},
			map[string]int{"a.go": 1, "b.go": 5},
			[]string{"b.go"}, []string{"a.go"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			worse, better := compareToBaseline(c.allowed, c.counts)
			if strings.Join(worse, ",") != strings.Join(c.worse, ",") {
				t.Errorf("worse = %v, want %v", worse, c.worse)
			}
			if strings.Join(better, ",") != strings.Join(c.better, ",") {
				t.Errorf("better = %v, want %v", better, c.better)
			}
		})
	}
}

func TestRuleTextSelfCheck(t *testing.T) {
	doc := "# Doc\n\n**[enforced: a-rule]** First line of the rule.\nSecond line.\n\n" +
		"**[practice]** Something else entirely.\n"

	got := ruleText(doc, "a-rule")
	if !strings.Contains(got, "First line of the rule. Second line.") {
		t.Errorf("did not flatten the rule's paragraph: %q", got)
	}
	if strings.Contains(got, "Something else entirely") {
		t.Errorf("ran past the blank line into the next paragraph: %q", got)
	}
	if missing := ruleText(doc, "absent-rule"); !strings.HasPrefix(missing, "(no paragraph") {
		t.Errorf("a missing tag should say so, got %q", missing)
	}
}

func TestHelpersSelfCheck(t *testing.T) {
	if !hasUnbreakableToken("// " + strings.Repeat("x", unbreakableToken+1)) {
		t.Error("a token past the limit should exempt its line")
	}
	if hasUnbreakableToken("// " + words(20)) {
		t.Error("ordinary words should not exempt a line")
	}
	if !hasPackagePrefix("opus: bad", "opus", "opus") {
		t.Error("the package's own prefix should be accepted")
	}
	if hasPackagePrefix("bad", "opus", "opus") {
		t.Error("an unprefixed message should not be accepted")
	}
	// A command is package main, so the directory names it instead.
	if !hasPackagePrefix("migrate-store: read", "main", "migrate-store") {
		t.Error("a command should be able to prefix with its directory")
	}
}

func TestClipSelfCheck(t *testing.T) {
	// Slicing by bytes cut multi-byte runes in half, producing mojibake in the
	// very message meant to explain a failure.
	long := strings.Repeat("é", 60)
	got := clip(long, 20)
	if !utf8.ValidString(got) {
		t.Errorf("clip produced invalid UTF-8: %q", got)
	}
	if n := utf8.RuneCountInString(got); n > 20 {
		t.Errorf("clip returned %d runes, want at most 20", n)
	}
	if short := clip("abc", 20); short != "abc" {
		t.Errorf("clip shortened a string that already fit: %q", short)
	}
	if !utf8.ValidString(truncate(strings.Repeat("ü", 80))) {
		t.Error("truncate produced invalid UTF-8")
	}
}

func TestRuleTextPrefersWholeSentences(t *testing.T) {
	first := "The rule itself, stated plainly in one sentence."
	doc := "**[enforced: a-rule]** " + first + " " + strings.Repeat("Filler prose. ", 30) + "\n\n"
	got := ruleText(doc, "a-rule")
	if got != first {
		t.Errorf("wanted the first sentence intact, got %q", got)
	}
	if strings.HasSuffix(got, "...") {
		t.Error("a sentence that fits should not be truncated")
	}
}
