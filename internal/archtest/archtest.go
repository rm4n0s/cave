// Package archtest checks the rules of docs/SPEC.md §4.2 over Cave's source: which package may
// import which, the imports and calls that are forbidden, the module's direct dependencies, and
// that every error Tag is asserted by a test. It reads the Go files with go/parser and never
// builds them, so the files of every platform are checked on any platform.
package archtest

import (
	"bufio"
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rm4n0s/errors"
)

// The Tags of this package's errors.
const (
	TagSourceWalkFailed   = "ArchtestSourceWalkFailed"
	TagSourceReadFailed   = "ArchtestSourceReadFailed"
	TagSourceParseFailed  = "ArchtestSourceParseFailed"
	TagGoModUnreadable    = "ArchtestGoModUnreadable"
	TagGoModWithoutModule = "ArchtestGoModWithoutModule"
)

// The import paths the rules name.
const (
	glutterModule = "github.com/rm4n0s/glutter"
	errorsModule  = "github.com/rm4n0s/errors"
)

// allowedDependencies are the modules go.mod may require directly (CLAUDE.md "Go conventions").
var allowedDependencies = []string{
	glutterModule,
	errorsModule,
	"golang.org/x/crypto",
	"golang.org/x/sys",
	"charm.land/bubbletea/v2",
	"charm.land/lipgloss/v2",
	// What Bubble Tea and Lip Gloss bring in, which the terminal UI uses for cell widths and for
	// cutting text that holds styles (CLAUDE.md: "and what these bring in").
	"github.com/charmbracelet/x/ansi",
}

// charmPrefixes are the import path prefixes of Bubble Tea, Lip Gloss and the rest of Charm.
var charmPrefixes = []string{"charm.land/", "github.com/charmbracelet/"}

// sourceDirs are the folders of the module that hold Cave's Go code.
var sourceDirs = []string{"cmd", "internal"}

// Violation is one place where the source breaks a rule of docs/SPEC.md §4.2.
type Violation struct {
	// Rule is the number of the rule in docs/SPEC.md §4.2.
	Rule int
	// File is the file's path from the module root, with slashes ("go.mod" for rule 8).
	File string
	// Line is the line in File.
	Line int
	// Message says what is wrong.
	Message string
}

// String formats the violation as "file:line: rule N: message".
func (v Violation) String() string {
	return v.File + ":" + strconv.Itoa(v.Line) + ": rule " + strconv.Itoa(v.Rule) + ": " + v.Message
}

// sourceFile is one parsed Go file of the module.
type sourceFile struct {
	// rel is the path from the module root, with slashes.
	rel string
	// test says whether it is a _test.go file.
	test bool
	// data is the file's content.
	data []byte
	fset *token.FileSet
	ast  *ast.File
}

// line returns the line of a position in the file.
func (f *sourceFile) line(pos token.Pos) int {
	return f.fset.Position(pos).Line
}

// importName returns the name under which the file imports path ("" when it does not, "." and
// "_" as written).
func (f *sourceFile) importName(path string) string {
	for _, spec := range f.ast.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil || p != path {
			continue
		}
		if spec.Name != nil {
			return spec.Name.Name
		}
		return p[strings.LastIndex(p, "/")+1:]
	}
	return ""
}

// loadSources parses every Go file under the source folders of root, skipping testdata folders.
// The files come sorted by path.
func loadSources(root string) ([]*sourceFile, error) {
	var files []*sourceFile
	fset := token.NewFileSet()
	tree := os.DirFS(root)
	for _, dir := range sourceDirs {
		if _, err := fs.Stat(tree, dir); os.IsNotExist(err) {
			continue
		}
		err := fs.WalkDir(tree, dir, func(rel string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return errors.NewErr(TagSourceWalkFailed, walkErr).SetMetadata("path", rel)
			}
			if d.IsDir() {
				if d.Name() == "testdata" {
					return fs.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(rel, ".go") {
				return nil
			}
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
			if err != nil {
				return errors.NewErr(TagSourceReadFailed, err).SetMetadata("path", rel)
			}
			parsed, err := parser.ParseFile(fset, rel, data, parser.ParseComments|parser.SkipObjectResolution)
			if err != nil {
				return errors.NewErr(TagSourceParseFailed, err).SetMetadata("path", rel)
			}
			files = append(files, &sourceFile{rel: rel, test: strings.HasSuffix(rel, "_test.go"), data: data, fset: fset, ast: parsed})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })
	return files, nil
}

// goMod is what the rules need of a go.mod file.
type goMod struct {
	// module is the module's path.
	module string
	// direct maps each directly required module to its line.
	direct map[string]int
}

// readGoMod reads the module path and the direct requirements of root's go.mod.
func readGoMod(root string) (*goMod, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return nil, errors.NewErr(TagGoModUnreadable, err).SetMetadata("root", root)
	}
	mod := &goMod{direct: map[string]int{}}
	inRequire := false
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for n := 1; scanner.Scan(); n++ {
		line := strings.TrimSpace(scanner.Text())
		indirect := strings.Contains(line, "// indirect")
		if i := strings.Index(line, "//"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		fields := strings.Fields(line)
		switch {
		case len(fields) == 0:
		case fields[0] == "module" && len(fields) >= 2:
			mod.module = strings.Trim(fields[1], `"`)
		case fields[0] == "require" && len(fields) == 2 && fields[1] == "(":
			inRequire = true
		case inRequire && fields[0] == ")":
			inRequire = false
		case fields[0] == "require" && len(fields) >= 3:
			if !indirect {
				mod.direct[fields[1]] = n
			}
		case inRequire && len(fields) >= 2:
			if !indirect {
				mod.direct[fields[0]] = n
			}
		}
	}
	if mod.module == "" {
		return nil, errors.New(TagGoModWithoutModule, "go.mod has no module line", "root", root)
	}
	return mod, nil
}

// hasPathPrefix says whether an import path is prefix itself or a package below it.
func hasPathPrefix(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

// CheckBoundaries checks rules 1 to 6, 8 and 9 of docs/SPEC.md §4.2 over the module at root and
// returns every place that breaks one, sorted by file and line. Rule 7 is ScanTags.
func CheckBoundaries(root string) ([]Violation, error) {
	mod, err := readGoMod(root)
	if err != nil {
		return nil, err
	}
	files, err := loadSources(root)
	if err != nil {
		return nil, err
	}
	var out []Violation
	for _, f := range files {
		out = append(out, checkImports(f, mod.module)...)
		out = append(out, checkCalls(f)...)
		out = append(out, checkInvisible(f)...)
	}
	for dep, line := range mod.direct {
		allowed := false
		for _, a := range allowedDependencies {
			if dep == a {
				allowed = true
			}
		}
		if !allowed {
			out = append(out, Violation{Rule: 8, File: "go.mod", Line: line, Message: "the dependency " + dep + " is not one CLAUDE.md allows"})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].Rule < out[j].Rule
	})
	return out, nil
}

// checkImports checks the imports of one file against rules 1 to 5.
func checkImports(f *sourceFile, module string) []Violation {
	inCore := strings.HasPrefix(f.rel, "internal/core/")
	inApp := strings.HasPrefix(f.rel, "internal/app/")
	inGUI := strings.HasPrefix(f.rel, "internal/gui/")
	inTUI := strings.HasPrefix(f.rel, "internal/tui/")
	inCmdCave := strings.HasPrefix(f.rel, "cmd/cave/")
	inCmdTUI := strings.HasPrefix(f.rel, "cmd/tcave/")

	var out []Violation
	add := func(rule int, spec *ast.ImportSpec, message string) {
		out = append(out, Violation{Rule: rule, File: f.rel, Line: f.line(spec.Pos()), Message: message})
	}
	for _, spec := range f.ast.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		isApp := hasPathPrefix(path, module+"/internal/app")
		isGUI := hasPathPrefix(path, module+"/internal/gui")
		isTUI := hasPathPrefix(path, module+"/internal/tui")
		isGlutter := hasPathPrefix(path, glutterModule)
		isCharm := false
		for _, p := range charmPrefixes {
			if strings.HasPrefix(path, p) {
				isCharm = true
			}
		}
		switch {
		case path == "C":
			add(5, spec, `import "C": Cave is cgo-free`)
		case path == "errors":
			add(4, spec, "the standard library errors package is imported; use "+errorsModule)
		case inCore && (isApp || isGUI || isTUI || isGlutter || isCharm):
			add(1, spec, "internal/core imports "+path)
		case inApp && (isGUI || isTUI || isGlutter || isCharm):
			add(2, spec, "internal/app imports "+path)
		case isGUI && !inGUI && !inCmdCave:
			add(3, spec, "only internal/gui and cmd/cave may import "+path)
		case isTUI && !inTUI && !inCmdTUI:
			add(3, spec, "only internal/tui and cmd/tcave may import "+path)
		case isGlutter && !inGUI && !inCmdCave:
			add(3, spec, "Glutter may be imported only under internal/gui and cmd/cave, not "+path+" here")
		case isCharm && !inTUI && !inCmdTUI:
			add(3, spec, "Bubble Tea and Lip Gloss may be imported only under internal/tui and cmd/tcave, not "+path+" here")
		}
	}
	return out
}

// checkCalls checks the calls and names of one file against rules 4 and 6.
func checkCalls(f *sourceFile) []Violation {
	if f.test {
		return nil
	}
	fmtName := f.importName("fmt")
	inSSHTest := strings.HasPrefix(f.rel, "internal/core/sshtest/")
	var out []Violation
	ast.Inspect(f.ast, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if x, ok := sel.X.(*ast.Ident); ok && fmtName != "" && x.Name == fmtName && sel.Sel.Name == "Errorf" {
			out = append(out, Violation{Rule: 4, File: f.rel, Line: f.line(sel.Pos()), Message: "fmt.Errorf is used; use " + errorsModule})
		}
		if sel.Sel.Name == "InsecureIgnoreHostKey" && !inSSHTest {
			out = append(out, Violation{Rule: 6, File: f.rel, Line: f.line(sel.Pos()), Message: "InsecureIgnoreHostKey outside tests and internal/core/sshtest"})
		}
		return true
	})
	return out
}

// checkInvisible checks one file against rule 9: no control or format character but the newline
// and the tab, and no invalid UTF-8. Such a character is written as an escape, so that the code
// a reviewer sees is the code the compiler gets. It reports a line once.
func checkInvisible(f *sourceFile) []Violation {
	var out []Violation
	line := 1
	reported := 0
	for i := 0; i < len(f.data); {
		r, size := utf8.DecodeRune(f.data[i:])
		i += size
		switch {
		case r == '\n':
			line++
		case r == '\t':
		case (r == utf8.RuneError && size == 1) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) ||
			unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r):
			if reported != line {
				reported = line
				out = append(out, Violation{Rule: 9, File: f.rel, Line: line,
					Message: "an invisible character (U+" + strings.ToUpper(strconv.FormatInt(int64(r), 16)) + "); write it as an escape"})
			}
		}
	}
	return out
}
