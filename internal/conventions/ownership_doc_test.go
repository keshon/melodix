package conventions

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// docs/ownership.md is the one document in this repository that claims, rule
// by rule, that something enforces it. A rule whose named enforcement has been
// renamed or deleted is worse than an unenforced rule: it reads as covered.
//
// This is the same bargain TestDocumentAndChecksAgree strikes for the
// conventions document, for the same reason. The audit that produced the
// ownership rules found a doc asserting that a deleted test "pins down the
// Opus-send contract", and the property it named had been gone for months.
var ownershipTestRef = regexp.MustCompile(`\b([a-z][a-z0-9]*)\.(Test[A-Za-z0-9_]+)`)

// bareTestRef catches a test named without its package, which the document
// does for the ones it has already attributed in the same paragraph.
var bareTestRef = regexp.MustCompile("`(Test[A-Za-z0-9_]+)`")

func TestOwnershipDocumentNamesRealTests(t *testing.T) {
	root := repoRoot(t)

	doc, err := os.ReadFile(filepath.Join(root, "docs", "ownership.md"))
	if err != nil {
		t.Fatalf("reading the ownership document: %v", err)
	}

	named := map[string]string{} // test name -> how the document wrote it
	for _, m := range ownershipTestRef.FindAllStringSubmatch(string(doc), -1) {
		named[m[2]] = m[1] + "." + m[2]
	}
	for _, m := range bareTestRef.FindAllStringSubmatch(string(doc), -1) {
		if _, ok := named[m[1]]; !ok {
			named[m[1]] = m[1]
		}
	}
	if len(named) == 0 {
		t.Fatal("the ownership document names no tests at all; either it lost its enforcement column or this check lost its regexp")
	}

	found := existingTestNames(t, root)

	var missing []string
	for name, written := range named {
		if !found[name] {
			missing = append(missing, written)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("docs/ownership.md names %d test(s) that do not exist:\n\t%s\n"+
			"A rule that names its enforcement and does not have it reads as covered, which is worse than saying nothing.",
			len(missing), strings.Join(missing, "\n\t"))
	}
}

func existingTestNames(t *testing.T, root string) map[string]bool {
	t.Helper()
	funcDecl := regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(`)

	found := map[string]bool{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		for _, m := range funcDecl.FindAllStringSubmatch(string(src), -1) {
			found[m[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}
	return found
}
