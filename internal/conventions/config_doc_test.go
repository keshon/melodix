package conventions

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Configuration is the one part of this project a user edits without reading
// any Go, so an undocumented knob is a knob nobody will find and a documented
// one that no longer exists is worse: it gets set, it does nothing, and the
// behaviour it was reached for stays broken.
//
// Both drifts are silent. A deleted field compiles, a renamed one compiles,
// and `caarlos0/env` ignores an environment variable nothing declares. Nothing
// anywhere notices except this.
var (
	envTag      = regexp.MustCompile(`env:"([A-Z][A-Z0-9_]*)"(?:\s+envDefault:"([^"]*)")?`)
	envTableRow = regexp.MustCompile("(?m)^\\|\\s*`([A-Z][A-Z0-9_]*)`\\s*\\|")
	envExample  = regexp.MustCompile(`(?m)^#?\s*([A-Z][A-Z0-9_]*)=`)
)

func TestEveryConfigKnobIsDocumented(t *testing.T) {
	root := repoRoot(t)

	cfg := readRepoFile(t, root, "internal", "config", "config.go")
	declared := map[string]string{}
	for _, m := range envTag.FindAllStringSubmatch(cfg, -1) {
		declared[m[1]] = m[2]
	}
	if len(declared) == 0 {
		t.Fatal("no env tags found in config.go; this check is guarding nothing")
	}

	// docker/.env.example is two files in one: the app's configuration, and
	// the handful of variables docker-compose itself reads to decide what to
	// build. The second set is legitimately not in internal/config, and is
	// listed here so that a fourth one has to be added deliberately rather
	// than by a regexp quietly widening.
	notAppConfig := map[string]bool{
		"ALIAS":   true, // container name and image tag
		"GIT":     true, // clone into ./src, or use what is already there
		"GIT_URL": true, // where to clone from
	}

	// Where each knob has to appear, and what shape to look for there.
	surfaces := []struct {
		path   []string
		named  func(string) map[string]bool
		what   string
		ignore map[string]bool
	}{
		{[]string{"docs", "running.md"}, rowsIn(envTableRow), "row in the variables table", nil},
		{[]string{".env.example"}, rowsIn(envExample), "a KEY= line", nil},
		{[]string{"docker", ".env.example"}, rowsIn(envExample), "a KEY= line", notAppConfig},
	}

	for _, s := range surfaces {
		doc := readRepoFile(t, root, s.path...)
		found := s.named(doc)
		where := filepath.Join(s.path...)

		var missing []string
		for name := range declared {
			if !found[name] {
				missing = append(missing, name)
			}
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			t.Errorf("%s has no %s for %d config knob(s):\n\t%s",
				where, s.what, len(missing), strings.Join(missing, "\n\t"))
		}

		var stale []string
		for name := range found {
			if s.ignore[name] {
				continue
			}
			if _, ok := declared[name]; !ok {
				stale = append(stale, name)
			}
		}
		sort.Strings(stale)
		if len(stale) > 0 {
			t.Errorf("%s documents %d variable(s) that internal/config no longer declares:\n\t%s\n"+
				"Setting one of these does nothing, which is a worse outcome than never having read about it.",
				where, len(stale), strings.Join(stale, "\n\t"))
		}
	}
}

// The default a user reads is the default they plan around, so it has to be
// the one the code will actually use.
func TestDocumentedDefaultsMatchTheCode(t *testing.T) {
	root := repoRoot(t)

	cfg := readRepoFile(t, root, "internal", "config", "config.go")
	declared := map[string]string{}
	for _, m := range envTag.FindAllStringSubmatch(cfg, -1) {
		declared[m[1]] = m[2]
	}

	// The table's third column, which is where the default is written.
	row := regexp.MustCompile("(?m)^\\|\\s*`([A-Z][A-Z0-9_]*)`\\s*\\|[^|]*\\|([^|]*)\\|\\s*$")
	doc := readRepoFile(t, root, "docs", "running.md")

	for _, m := range row.FindAllStringSubmatch(doc, -1) {
		name, cell := m[1], strings.TrimSpace(m[2])
		want, ok := declared[name]
		if !ok {
			continue // reported by TestEveryConfigKnobIsDocumented
		}
		if want == "" {
			// No envDefault: the zero value. The table writes that as "(none)".
			if !strings.Contains(cell, "(none)") {
				t.Errorf("%s has no default in the code, but running.md says %s; write (none)", name, cell)
			}
			continue
		}
		// The cell may annotate the value -- `2147483648` (2 GiB) -- so the
		// check is that the code's default is quoted in it, not that the cell
		// is nothing else.
		if !strings.Contains(cell, "`"+want+"`") {
			t.Errorf("running.md gives %s a default of %s; internal/config declares %q",
				name, cell, want)
		}
	}
}

func readRepoFile(t *testing.T, root string, parts ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{root}, parts...)...)
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", filepath.Join(parts...), err)
	}
	return string(src)
}

func rowsIn(re *regexp.Regexp) func(string) map[string]bool {
	return func(doc string) map[string]bool {
		out := map[string]bool{}
		for _, m := range re.FindAllStringSubmatch(doc, -1) {
			out[m[1]] = true
		}
		return out
	}
}
