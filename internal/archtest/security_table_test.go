package archtest

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// cited is one test that the table of docs/SECURITY.md names: the file and the function.
var cited = regexp.MustCompile("`([^`]+_test\\.go)` → `(Test[A-Za-z0-9_]+)`")

// TestTheSecurityTableNamesRealTests reads the table "Rules and the tests that prove them" of
// docs/SECURITY.md and checks that every test it names is a function of the file it names, so that
// a renamed or deleted test cannot stay in the table.
func TestTheSecurityTableNamesRealTests(t *testing.T) {
	root := moduleRoot(t)
	text, err := os.ReadFile(filepath.Join(root, "docs", "SECURITY.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, table, found := strings.Cut(string(text), "## Rules and the tests that prove them")
	if !found {
		t.Fatal("docs/SECURITY.md has no table of rules and tests")
	}
	matches := cited.FindAllStringSubmatch(table, -1)
	if len(matches) < 40 {
		t.Fatalf("the table names %d tests, want at least 40", len(matches))
	}
	rows := 0
	for _, line := range strings.Split(table, "\n") {
		if strings.HasPrefix(line, "| **") {
			rows++
			if !cited.MatchString(line) {
				t.Errorf("the row names no test: %.80s…", line)
			}
		}
	}
	if rows < 25 {
		t.Errorf("the table has %d rows, want at least 25", rows)
	}
	sources := map[string]string{}
	for _, m := range matches {
		file, name := m[1], m[2]
		source, ok := sources[file]
		if !ok {
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
			if err != nil {
				t.Errorf("the table names %s, which cannot be read: %v", file, err)
				sources[file] = "\x00"
				continue
			}
			source = string(data)
			sources[file] = source
		}
		if !strings.Contains(source, "\nfunc "+name+"(t *testing.T)") {
			t.Errorf("%s has no test %s, which the table names", file, name)
		}
	}
}
