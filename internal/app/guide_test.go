package app

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/rm4n0s/cave/internal/core/settings"
)

// updateGoldens rewrites the golden files instead of comparing with them:
// go test ./internal/app -run Golden -update-goldens. Read every file that changes.
var updateGoldens = flag.Bool("update-goldens", false, "rewrite the golden files")

// readDoc reads a file of the repository.
func readDoc(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(moduleRoot(t), filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestUsageGolden(t *testing.T) {
	for _, program := range []string{"cave", "tcave"} {
		for _, goos := range []string{"linux", "darwin", "windows"} {
			t.Run(program+" "+goos, func(t *testing.T) {
				got := usageFor(program, goos)
				path := filepath.Join("testdata", "goldens", "usage-"+program+"-"+goos+".txt")
				if *updateGoldens {
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
						t.Fatal(err)
					}
					return
				}
				want, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if got != string(want) {
					t.Errorf("the usage of %s on %s is not the golden file %s:\n%s", program, goos, path, got)
				}
			})
		}
	}
}

func TestUsageNamesTheFoldersOfThePlatform(t *testing.T) {
	for goos, want := range map[string][]string{
		"linux":   {"$XDG_CONFIG_HOME/cave, else ~/.config/cave", "$XDG_STATE_HOME/cave, else ~/.local/state/cave"},
		"darwin":  {"~/Library/Application Support/Cave", "~/Library/Application Support/Cave/State"},
		"windows": {`%AppData%\Cave`, `%LocalAppData%\Cave`},
	} {
		text := usageFor("cave", goos)
		for _, folder := range want {
			if !strings.Contains(text, folder) {
				t.Errorf("the usage on %s does not name %s:\n%s", goos, folder, text)
			}
		}
	}
	if Usage("cave") != usageFor("cave", runtime.GOOS) {
		t.Error("Usage is not the usage of this system")
	}
}

func TestTheGuideNamesEverySetting(t *testing.T) {
	guide := readDoc(t, "docs/GUIDE.md")
	for _, def := range settings.Schema() {
		if !strings.Contains(guide, "`"+def.Key+"`") {
			t.Errorf("docs/GUIDE.md does not name the setting `%s` in backticks", def.Key)
		}
	}
}

func TestTheGuideNamesEveryFlag(t *testing.T) {
	guide := readDoc(t, "docs/GUIDE.md")
	var f Flags
	var level string
	flagSet("cave", &f, &level).VisitAll(func(fl *flag.Flag) {
		if !strings.Contains(guide, "`--"+fl.Name) {
			t.Errorf("docs/GUIDE.md does not name the flag --%s", fl.Name)
		}
	})
	if !strings.Contains(guide, "`--help`") {
		t.Error("docs/GUIDE.md does not name --help")
	}
}

// link finds the targets of the Markdown links and images of a text.
var link = regexp.MustCompile(`\]\(([^)\s]+)\)`)

// slug is the anchor GitHub makes for a heading.
func slug(heading string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(heading)) {
		switch {
		case r == ' ' || r == '-':
			b.WriteByte('-')
		case r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r > 127:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func TestTheReadmeLinksExist(t *testing.T) {
	root := moduleRoot(t)
	for _, doc := range []string{"README.md", "docs/GUIDE.md", "docs/SECURITY.md"} {
		text := readDoc(t, doc)
		anchors := map[string]bool{}
		for _, line := range strings.Split(text, "\n") {
			if heading, ok := strings.CutPrefix(line, "#"); ok {
				anchors[slug(strings.TrimLeft(heading, "# "))] = true
			}
		}
		links := link.FindAllStringSubmatch(text, -1)
		if len(links) == 0 {
			t.Errorf("%s has no links to look at", doc)
		}
		for _, match := range links {
			target := match[1]
			if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			file, fragment, _ := strings.Cut(target, "#")
			if file == "" {
				if !anchors[fragment] {
					t.Errorf("%s links to #%s, and no heading has that anchor", doc, fragment)
				}
				continue
			}
			if _, err := os.Stat(filepath.Join(root, filepath.Dir(filepath.FromSlash(doc)), filepath.FromSlash(file))); err != nil {
				t.Errorf("%s links to %s, which does not exist", doc, target)
			}
		}
	}
}
