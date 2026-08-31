package conventions

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// The checks below ratchet: baseline.json records what each file owed when a
// rule was introduced, and a rule fails only when a file gets worse. New files
// start at zero, so new code meets the rule in full while old code is only
// required not to rot. That is what makes it possible to adopt a rule on a live
// codebase without a repo-wide edit nobody can review.
//
// Frozen identifiers are the exception and carry no baseline: their whole point
// is that they can never change. See TestFrozenIdentifiers.
//
// To accept the current state after fixing (or knowingly adding) violations:
//
//	CONVENTIONS_UPDATE=1 go test ./internal/conventions/
//
// Do NOT reach for that to silence a failure you have not read. The number
// going up is the finding; the baseline is only bookkeeping.

// --- project configuration ---
//
// Everything specific to this repository lives in this block and in the two
// maps above TestFrozenIdentifiers. To reuse this package elsewhere: copy the
// directory, edit these four fields, then rewrite frozenPackages and
// frozenLiterals for that project's persisted strings (or delete both tests if
// it has none). Run CONVENTIONS_UPDATE=1 once to record its baseline. Nothing
// else is Melodix-shaped.
var project = struct {
	// docPath locates the conventions document from the repo root. The wording
	// of every enforced rule is read out of it, so moving the file means
	// changing this.
	docPath []string
	// libraryPrefix scopes the rules that only apply to the reusable surface —
	// error prefixes and the density scorecard. Empty means the whole tree.
	libraryPrefix string
	// skipDirs are paths that are not ours to hold to these rules.
	skipDirs []string
	// bannedLibraryImports are packages the library surface must never import,
	// checked by TestLibraryStaysDiscordFree.
	bannedLibraryImports []string
}{
	docPath:              []string{"docs", "conventions.md"},
	libraryPrefix:        "pkg/music/",
	skipDirs:             []string{"pkg/discordgo-fork-dev", ".git"},
	bannedLibraryImports: []string{"discordgo", "melodix/internal"},
}

// maxCommentCols is the wrap width docs/conventions.md states for comments. A
// tab counts as one column: the rule is about wrapping prose, not about how
// deeply the code around it is indented.
const maxCommentCols = 80

// unbreakableToken is the length past which a word is assumed to be a URL or an
// identifier that cannot be wrapped, exempting its line. A rule that punishes
// an unbreakable token teaches people to ignore the rule.
const unbreakableToken = 30

type violation struct {
	file   string
	line   int
	detail string
}

// A rule's name is also its tag in docs/conventions.md, written there as
// **[enforced: <name>]**. The wording of the rule lives only in the document
// and is read back from it for failure messages, so there is one copy of the
// sentence and it cannot drift from the check. TestDocumentAndChecksAgree
// keeps the two sets of names in step.
type rule struct {
	name string
	// claims are values this check implements as a constant and the document
	// states in prose. The constant is the source of truth; the claim is what
	// makes the document unable to disagree with it in silence. Only list a
	// value the prose actually commits to — unbreakableToken is not here
	// because the paragraph says "a long identifier", not a number.
	claims []string
	scan   func(t *testing.T, files []goFile) []violation
}

type goFile struct {
	// path is slash-separated and relative to the repo root, so baselines are
	// identical on Windows and Linux.
	path  string
	pkg   string
	lines []string
}

func rules() []rule {
	return []rule{
		{name: "comment-width", claims: []string{strconv.Itoa(maxCommentCols)}, scan: scanCommentWidth},
		{name: "log-event-naming", scan: scanLogEvents},
		{name: "error-prefix", scan: scanErrorPrefix},
		{name: "file-headers", scan: scanFileHeaders},
	}
}

// ownedElsewhere are rules this package tags in the document but checks in a
// test of its own rather than through rules().
var ownedElsewhere = []string{"frozen-identifiers", "discord-free"}

// checkedByOtherTools are tags in the document whose enforcement lives outside
// this package. Each names the file that must prove the tool actually runs:
// without that, adding a tag here plus a line in the document would buy the
// full appearance of enforcement with nothing behind it — the exact failure
// this package exists to prevent, reachable in two lines.
var checkedByOtherTools = map[string]toolEvidence{
	"golangci": {
		file: []string{".golangci.yml"},
		want: []string{"enable:", "staticcheck", "govet"},
	},
	"race": {
		file: []string{".github", "workflows", "build.yml"},
		want: []string{"-race"},
	},
}

// toolEvidence is the file that must exist and the strings that must appear in
// it for a delegated tag to count as enforced.
type toolEvidence struct {
	file []string
	want []string
}

func TestConventions(t *testing.T) {
	root := repoRoot(t)
	files := collectGoFiles(t, root)
	if len(files) < 50 {
		t.Fatalf("only %d Go files found under %s — the walk is wrong", len(files), root)
	}

	doc := loadDoc(t, root)
	base := loadBaseline(t, root)
	found := map[string]map[string]int{}
	update := os.Getenv("CONVENTIONS_UPDATE") != ""
	if update && os.Getenv("CI") != "" {
		t.Fatal("CONVENTIONS_UPDATE is set in CI, which would rewrite the " +
			"baseline instead of checking against it — remove it from the " +
			"workflow environment")
	}

	for _, r := range rules() {
		vs := r.scan(t, files)
		counts := map[string]int{}
		byFile := map[string][]violation{}
		for _, v := range vs {
			counts[v.file]++
			byFile[v.file] = append(byFile[v.file], v)
		}
		found[r.name] = counts
		if update {
			continue
		}
		checkRatchet(t, r, ruleText(doc, r.name), base[r.name], counts, byFile)
	}

	if update {
		writeBaseline(t, root, found)
		// Deliberately not a passing run. An update skips the ratchet entirely,
		// so a green exit here would mean one stray environment variable — a
		// shell profile, a CI block, an agent's environment — silently disables
		// every ratcheted rule with no signal anywhere. Failing keeps the escape
		// hatch a thing you do on purpose and then look at.
		t.Errorf("baseline.json rewritten from the current tree; "+
			"re-run without CONVENTIONS_UPDATE to verify, and review the diff "+
			"before committing it (%d rules recorded)", len(found))
	}
	reportScorecard(t, base, found, files)
}

// checkRatchet fails on any file that got worse, and reports the ones that got
// better so the baseline can be tightened rather than quietly drifting loose.
func checkRatchet(t *testing.T, r rule, text string, allowed, counts map[string]int, byFile map[string][]violation) {
	t.Helper()
	worse, better := compareToBaseline(allowed, counts)

	for _, file := range worse {
		t.Errorf("%s: %s went from %d to %d violations\n  rule: %q\n%s",
			r.name, file, allowed[file], counts[file], text, sample(byFile[file]))
	}
	if len(better) > 0 && len(worse) == 0 {
		t.Logf("%s: %d file(s) improved — run CONVENTIONS_UPDATE=1 to lock the gain in: %s",
			r.name, len(better), strings.Join(better, ", "))
	}
}

// compareToBaseline splits files into those that exceeded their allowance and
// those that beat it. It takes no *testing.T so the ratchet's arithmetic — the
// part every rule depends on — can be asserted directly in selfcheck_test.go.
func compareToBaseline(allowed, counts map[string]int) (worse, better []string) {
	for file, n := range counts {
		if n > allowed[file] {
			worse = append(worse, file)
		}
	}
	for file, n := range allowed {
		if counts[file] < n {
			better = append(better, file)
		}
	}
	sort.Strings(worse)
	sort.Strings(better)
	return worse, better
}

func sample(vs []violation) string {
	sort.Slice(vs, func(i, j int) bool { return vs[i].line < vs[j].line })
	var b strings.Builder
	for i, v := range vs {
		if i == 5 {
			fmt.Fprintf(&b, "    ... and %d more\n", len(vs)-i)
			break
		}
		fmt.Fprintf(&b, "    %s:%d %s\n", v.file, v.line, v.detail)
	}
	return b.String()
}

// --- rules ---

func scanCommentWidth(_ *testing.T, files []goFile) []violation {
	var out []violation
	for _, f := range files {
		for i, line := range f.lines {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "//") || isDirective(trimmed) {
				continue
			}
			if utf8.RuneCountInString(line) <= maxCommentCols {
				continue
			}
			if hasUnbreakableToken(line) {
				continue
			}
			out = append(out, violation{f.path, i + 1,
				fmt.Sprintf("%d cols", utf8.RuneCountInString(line))})
		}
	}
	return out
}

// isDirective reports whether a comment is a tool directive rather than prose.
// Wrapping one breaks it, so the width rule cannot apply.
func isDirective(trimmed string) bool {
	for _, p := range []string{"//go:", "//nolint:", "//lint:", "//export "} {
		if strings.HasPrefix(trimmed, p) {
			return true
		}
	}
	return false
}

// hasUnbreakableToken reports whether a line is over-length only because of
// something that cannot be wrapped. Exempting the whole line on sight was the
// trivial evasion — one long identifier licensed any amount of prose after it —
// so the token is discounted and the rest of the line still has to fit.
func hasUnbreakableToken(line string) bool {
	longest := 0
	for _, field := range strings.Fields(line) {
		if n := utf8.RuneCountInString(field); n > longest {
			longest = n
		}
	}
	if longest <= unbreakableToken {
		return false
	}
	return utf8.RuneCountInString(line)-longest <= maxCommentCols
}

var (
	msgCall     = regexp.MustCompile(`\.Msg\(\s*"([^"]*)"\s*\)`)
	msgAnyCall  = regexp.MustCompile(`\.Msg\(`)
	msgfCall    = regexp.MustCompile(`\.Msgf\(`)
	snakeEvent  = regexp.MustCompile(`^[a-z0-9]+(_[a-z0-9]+)*$`)
	errSentinel = regexp.MustCompile(`\bErr[A-Z]\w*\s*=\s*errors\.New\(`)
	errLiteral  = regexp.MustCompile(`(?:errors\.New|fmt\.Errorf)\(\s*"([^"]{3,})"`)
)

func scanLogEvents(_ *testing.T, files []goFile) []violation {
	var out []violation
	for _, f := range files {
		for i, line := range f.lines {
			for _, m := range msgCall.FindAllStringSubmatch(line, -1) {
				if !snakeEvent.MatchString(m[1]) {
					out = append(out, violation{f.path, i + 1,
						fmt.Sprintf("event %q is not snake_case", m[1])})
				}
			}
			if msgfCall.MatchString(line) {
				out = append(out, violation{f.path, i + 1,
					"Msgf interpolates the message; use structured fields"})
			}
			// A Msg call whose argument is not a literal is an event name
			// computed at runtime — unsearchable, and invisible to the check
			// above, which only sees literals. Counting it here is what found a
			// live one hiding in the discordgo log bridge. Note this comment
			// avoids spelling the call out: the scan reads source lines, so
			// writing it would flag this very file.
			if msgAnyCall.MatchString(line) && !msgCall.MatchString(line) {
				out = append(out, violation{f.path, i + 1,
					"Msg with a non-literal event name cannot be grepped"})
			}
		}
	}
	return out
}

var fileHeader = regexp.MustCompile(`^//\s*(FILE|File|Path):`)

// scanFileHeaders catches a comment naming the file it sits in. Nothing checks
// such a header, so it survives every rename: the one this repo carried named a
// path that had never existed in either project.
func scanFileHeaders(_ *testing.T, files []goFile) []violation {
	var out []violation
	for _, f := range files {
		for i, line := range f.lines {
			if fileHeader.MatchString(strings.TrimSpace(line)) {
				out = append(out, violation{f.path, i + 1, "file-path header"})
			}
		}
	}
	return out
}

// scanErrorPrefix covers pkg/music only: that is the library surface the rule
// names. Exported sentinels are exempt because their text doubles as the string
// a user is shown, which the same section of the document calls for — see the
// note on sentinels there.
func scanErrorPrefix(_ *testing.T, files []goFile) []violation {
	var out []violation
	for _, f := range files {
		if !strings.HasPrefix(f.path, project.libraryPrefix) || strings.HasSuffix(f.path, "_test.go") {
			continue
		}
		for i, line := range f.lines {
			if errSentinel.MatchString(line) {
				continue
			}
			for _, m := range errLiteral.FindAllStringSubmatch(line, -1) {
				if !hasPackagePrefix(m[1], f.pkg, path.Base(path.Dir(f.path))) {
					out = append(out, violation{f.path, i + 1,
						fmt.Sprintf("%q does not start with %q", truncate(m[1]), f.pkg+": ")})
				}
			}
		}
	}
	return out
}

// hasPackagePrefix accepts the package's own name, the directory it lives in,
// or a prefix supplied at runtime. The directory is accepted because a command
// is always package main: "main: read store" names nothing a reader can act on,
// while "migrate-store: read store" names the binary that printed it.
func hasPackagePrefix(msg, pkg, dir string) bool {
	return strings.HasPrefix(msg, pkg+":") ||
		(dir != "" && strings.HasPrefix(msg, dir+":")) ||
		strings.HasPrefix(msg, "%s:") ||
		strings.HasPrefix(msg, "%w")
}

// truncate shortens by runes, not bytes: slicing a multi-byte character in
// half produces mojibake in the very message meant to explain the failure.
func truncate(s string) string {
	return clip(s, 48)
}

func clip(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return strings.TrimRight(string(r[:limit-3]), " ") + "..."
}

// --- frozen identifiers ---

// --- project-specific invariants: rewrite or delete these when porting ---

// frozenPackages are the packages whose exported string constants are frozen,
// and the pinned value of every one of them. The set is derived from the source
// rather than listed by hand, so a constant added later cannot ship unpinned —
// which is what the document promises and what a hand-written list could not
// deliver.
var frozenPackages = map[string]map[string]string{
	"pkg/music/sources": {
		"ParserYtnativeLink": "ytnative-link",
		"ParserScnativeLink": "scnative-link",
		"ParserKkdaiLink":    "kkdai-link",
		"ParserKkdaiPipe":    "kkdai-pipe",
		"ParserYtdlpLink":    "ytdlp-link",
		"ParserYtdlpPipe":    "ytdlp-pipe",
		"ParserFFmpegLink":   "ffmpeg-link",
		"Auto":               "auto",
		"YouTube":            "youtube",
		"Radio":              "radio",
		"SoundCloud":         "soundcloud",
	},
}

// frozenLiterals are unexported values that outlive the code holding them and
// so cannot be read as constants from another package. file is relative to the
// repo root; each entry must appear in it verbatim.
var frozenLiterals = map[string][]string{
	"internal/command/music/search/search.go": {
		`sourceYouTube    = "yt"`,
		`sourceSoundCloud = "sc"`,
	},
}

// TestFrozenIdentifiers pins the strings that outlive the code holding them.
// Parser keys and source names sit in guild playback history and in registered
// slash-command choices; the /search source tags sit inside component ids on
// choosers already posted in channels, which come back when someone presses a
// button long after a restart. Renaming any of them silently breaks data that
// is already out there, so this test has no baseline and never gets one.
//
// It reads the constants out of the source with go/ast rather than trusting a
// list, so the three ways this can go wrong all fail here: a value changed, a
// pinned constant deleted, or — the one a hand-written list misses — a new
// exported constant added to a frozen package and never pinned at all.
func TestFrozenIdentifiers(t *testing.T) {
	root := repoRoot(t)

	for pkgPath, want := range frozenPackages {
		got := exportedStringConsts(t, filepath.Join(root, filepath.FromSlash(pkgPath)))
		for name, wantVal := range want {
			gotVal, ok := got[name]
			if !ok {
				t.Errorf("%s.%s is pinned but no longer exists — it is persisted "+
					"in playback history; deleting it strands that data", pkgPath, name)
				continue
			}
			if gotVal != wantVal {
				t.Errorf("%s.%s is %q, was %q — this string is persisted; add a new "+
					"constant instead of changing this one", pkgPath, name, gotVal, wantVal)
			}
		}
		for name, gotVal := range got {
			if _, ok := want[name]; !ok {
				t.Errorf("%s.%s = %q is a new exported constant in a frozen package "+
					"and is not pinned. If it is persisted or shown as a command choice, "+
					"add it to frozenPackages in the same commit; if it is not, move it "+
					"out of this package.", pkgPath, name, gotVal)
			}
		}
	}

	for rel, wants := range frozenLiterals {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		for _, want := range wants {
			if !strings.Contains(string(src), want) {
				t.Errorf("%s no longer contains %q — component ids already posted "+
					"in channels carry that tag and come back when a button is pressed",
					rel, want)
			}
		}
	}
}

// exportedStringConsts returns every exported untyped string constant declared
// in the package at dir, keyed by name.
func exportedStringConsts(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	fset := token.NewFileSet()
	out := map[string]string{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, ident := range vs.Names {
					if !ident.IsExported() || i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					val, err := strconv.Unquote(lit.Value)
					if err != nil {
						continue
					}
					out[ident.Name] = val
				}
			}
		}
	}
	return out
}

// TestLibraryStaysDiscordFree holds the boundary that makes the library
// reusable and the CLI possible: the engine knows nothing about Discord. It
// carries no baseline because a single import would undo the property — there
// is nothing to ratchet toward. Imports are read from the AST rather than
// matched in text, so a module path inside an ordinary string cannot trip it
// and an aliased import cannot hide from it.
func TestLibraryStaysDiscordFree(t *testing.T) {
	root := repoRoot(t)
	fset := token.NewFileSet()
	for _, f := range collectGoFiles(t, root) {
		if !strings.HasPrefix(f.path, project.libraryPrefix) {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(root, filepath.FromSlash(f.path)),
			nil, parser.ImportsOnly)
		if err != nil {
			t.Errorf("parse %s: %v", f.path, err)
			continue
		}
		for _, imp := range file.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				continue
			}
			for _, bad := range project.bannedLibraryImports {
				if strings.Contains(path, bad) {
					t.Errorf("%s imports %q — %s must stay Discord-free (the CLI is "+
						"the proof it holds); Discord code belongs in internal/",
						f.path, path, project.libraryPrefix)
				}
			}
		}
	}
}

// --- the document ---

var enforcedTag = regexp.MustCompile(`\*\*\[enforced: ([a-z-]+)\]\*\*`)

func docPath(root string) string {
	return filepath.Join(append([]string{root}, project.docPath...)...)
}

func loadDoc(t *testing.T, root string) string {
	t.Helper()
	src, err := os.ReadFile(docPath(root))
	if err != nil {
		t.Fatalf("the conventions document is not at %s: %v — every enforced "+
			"rule reads its wording from there, so if the document moved, "+
			"update project.docPath", filepath.Join(project.docPath...), err)
	}
	return strings.ReplaceAll(string(src), "\r\n", "\n")
}

// ruleText returns the paragraph documenting a rule, flattened to one line for
// a failure message. The document is the only place the wording lives.
func ruleText(doc, name string) string {
	marker := "**[enforced: " + name + "]**"
	i := strings.Index(doc, marker)
	if i < 0 {
		return "(no paragraph tagged " + marker + " in docs/conventions.md)"
	}
	rest := doc[i+len(marker):]
	if end := strings.Index(rest, "\n\n"); end >= 0 {
		rest = rest[:end]
	}
	flat := strings.Join(strings.Fields(rest), " ")
	flat = strings.NewReplacer("`", "", "*", "").Replace(flat)
	// Prefer whole sentences: a paragraph cut mid-word reads worse than the one
	// sentence that states the rule, which is almost always the first.
	if len(flat) > 160 {
		if cut := strings.Index(flat, ". "); cut > 0 && cut < 160 {
			return flat[:cut+1]
		}
		flat = clip(flat, 160)
	}
	return flat
}

// TestDocumentAndChecksAgree is what stops docs/conventions.md and this file
// from becoming two sources of truth that disagree. Every rule tagged
// **[enforced: x]** in the document must be checked by something, and every
// check here must be tagged in the document — so a rule cannot be advertised as
// enforced while nothing runs, and a check cannot quietly enforce something the
// document never told anyone about.
func TestDocumentAndChecksAgree(t *testing.T) {
	doc := loadDoc(t, repoRoot(t))

	tagged := map[string]bool{}
	for _, m := range enforcedTag.FindAllStringSubmatch(doc, -1) {
		tagged[m[1]] = true
	}
	if len(tagged) == 0 {
		t.Fatal("no **[enforced: name]** tags found — has the document's tier " +
			"notation changed? The checks and the prose have to move together.")
	}

	implemented := map[string]bool{}
	for _, r := range rules() {
		implemented[r.name] = true
	}
	for _, name := range ownedElsewhere {
		implemented[name] = true
	}
	external := map[string]bool{}
	for name := range checkedByOtherTools {
		external[name] = true
	}

	for name := range implemented {
		if !tagged[name] {
			t.Errorf("check %q runs but no rule in docs/conventions.md is tagged "+
				"**[enforced: %s]** — the document does not tell anyone this is checked",
				name, name)
		}
	}
	for name := range tagged {
		if !implemented[name] && !external[name] {
			t.Errorf("docs/conventions.md advertises **[enforced: %s]** but nothing "+
				"checks it — either write the check or drop the rule to [invariant]",
				name)
		}
	}

	// A tag delegated to another tool has to show that the tool is actually
	// wired up, or the delegation is just a claim.
	root := repoRoot(t)
	for name, ev := range checkedByOtherTools {
		if !tagged[name] {
			continue
		}
		rel := filepath.Join(ev.file...)
		src, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Errorf("docs/conventions.md delegates **[enforced: %s]** to %s, "+
				"which is not there: %v", name, rel, err)
			continue
		}
		for _, want := range ev.want {
			if !strings.Contains(string(src), want) {
				t.Errorf("docs/conventions.md delegates **[enforced: %s]** to %s, "+
					"but it no longer contains %q — the rule is advertised as "+
					"enforced with nothing running", name, rel, want)
			}
		}
	}

	// A tagged rule with no readable paragraph would leave failures quoting
	// nothing, which is how the wording drifts back into the code.
	for name := range tagged {
		if strings.HasPrefix(ruleText(doc, name), "(no paragraph") {
			t.Errorf("rule %q has a tag but no paragraph to quote", name)
		}
	}

	// The prose must not state a threshold the check does not implement. The
	// constant decides behaviour; this stops the document from quietly
	// promising a different number than the one the build enforces.
	for _, r := range rules() {
		text := ruleText(doc, r.name)
		for _, claim := range r.claims {
			if !strings.Contains(text, claim) {
				t.Errorf("check %q enforces %s but its paragraph in docs/conventions.md "+
					"never says so — the document and the build disagree on the number. "+
					"The paragraph reads: %q", r.name, claim, text)
			}
		}
	}
}

// --- scorecard ---

// reportScorecard prints where the codebase stands. Comment density is here
// rather than in rules() on purpose: no threshold separates a file that
// explains a hard decision from one that repeats itself, so gating it would
// pressure people to delete comments that earn their place. It is reported so
// drift is visible and judged by a person, which is the honest arrangement.
func reportScorecard(t *testing.T, base, found map[string]map[string]int, files []goFile) {
	t.Helper()
	var b strings.Builder
	b.WriteString("\n  convention scorecard\n")
	for _, r := range rules() {
		fmt.Fprintf(&b, "    %-18s %4d violations (baseline %d)\n",
			r.name, total(found[r.name]), total(base[r.name]))
	}

	type dens struct {
		path  string
		ratio float64
	}
	var ds []dens
	for _, f := range files {
		if !strings.HasPrefix(f.path, project.libraryPrefix) || strings.HasSuffix(f.path, "_test.go") {
			continue
		}
		var c, code int
		for _, l := range f.lines {
			switch {
			case strings.TrimSpace(l) == "":
			case strings.HasPrefix(strings.TrimSpace(l), "//"):
				c++
			default:
				code++
			}
		}
		if code >= 40 {
			ds = append(ds, dens{f.path, float64(c) / float64(code)})
		}
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i].ratio > ds[j].ratio })
	fmt.Fprintf(&b, "    comment density, %s (not a gate — read them):\n", project.libraryPrefix)
	for i, d := range ds {
		if i == 5 {
			break
		}
		fmt.Fprintf(&b, "      %.2f  %s\n", d.ratio, d.path)
	}
	t.Log(b.String())
}

func total(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

// --- plumbing ---

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test directory")
		}
		dir = parent
	}
}

func collectGoFiles(t *testing.T, root string) []goFile {
	t.Helper()
	var out []goFile
	pkgClause := regexp.MustCompile(`(?m)^package (\w+)`)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		for _, skip := range project.skipDirs {
			if rel == skip || strings.HasPrefix(rel, skip+"/") {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		pkg := ""
		if m := pkgClause.FindSubmatch(src); m != nil {
			pkg = string(m[1])
		}
		text := strings.ReplaceAll(string(src), "\r\n", "\n")
		out = append(out, goFile{path: rel, pkg: pkg, lines: strings.Split(text, "\n")})
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	return out
}

func baselinePath(root string) string {
	return filepath.Join(root, "internal", "conventions", "baseline.json")
}

func loadBaseline(t *testing.T, root string) map[string]map[string]int {
	t.Helper()
	src, err := os.ReadFile(baselinePath(root))
	if os.IsNotExist(err) {
		return map[string]map[string]int{}
	}
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	var out map[string]map[string]int
	if err := json.Unmarshal(src, &out); err != nil {
		t.Fatalf("parse baseline: %v", err)
	}
	return out
}

func writeBaseline(t *testing.T, root string, found map[string]map[string]int) {
	t.Helper()
	trimmed := map[string]map[string]int{}
	for rule, counts := range found {
		kept := map[string]int{}
		for file, n := range counts {
			if n > 0 {
				kept[file] = n
			}
		}
		trimmed[rule] = kept
	}
	src, err := json.MarshalIndent(trimmed, "", "  ")
	if err != nil {
		t.Fatalf("encode baseline: %v", err)
	}
	if err := os.WriteFile(baselinePath(root), append(src, '\n'), 0o644); err != nil {
		t.Fatalf("write baseline: %v", err)
	}
}
