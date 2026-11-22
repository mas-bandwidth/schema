package ci

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// THE CLI WRITES THROUGH ONE PUBLISHER.
//
// Four commands once cut the file they were rewriting down to the size limit
// they ran under, because each wrote into the file in place. Every write the
// CLI makes now goes through internal/atomicfile, which stages the new
// content beside the file and renames it in, and cmd/schema's
// TestNoCommandDamagesAFileItRewrites fails each command at every point of
// that publish. This gate keeps a new command from skipping both: it parses
// cmd/schema and every package of this module cmd/schema links (so a new
// output package is covered the day it is imported) and refuses a file
// operation that changes a file or an entry outside the publisher.
//
// It is parse-only and reads no types, so it goes by spelling:
//   - any use of the os package's file-changing functions (publisherOSFuncs),
//     called or passed as a value, under whatever name os is imported;
//   - any call of a method with one of those names on a value (an *os.Root,
//     an *os.File): atomicfile's own verbs are spelled otherwise (Write,
//     Delete, Respell) so that this stays exact.
//
// os.OpenFile is let through only with the literal flag os.O_RDONLY. Making
// a directory (Mkdir, MkdirAll) changes no file and is not refused.
// publisherAllowed lists the functions that may change entries directly,
// each with why; an entry that no longer matches anything fails the gate.

// publisherOSFuncs are the os functions, and the method names, that change a
// file or a directory entry.
var publisherOSFuncs = map[string]bool{
	"WriteFile": true, "Create": true, "OpenFile": true, "CreateTemp": true,
	"Rename": true, "Remove": true, "RemoveAll": true, "Truncate": true,
	"Link": true, "Symlink": true, "Chmod": true, "Chtimes": true, "Chown": true,
	"CopyFS": true,
}

// publisherAllowed is "file: function" to the reason it may change entries
// without the publisher.
var publisherAllowed = map[string]string{
	"cmd/schema/main.go: writeNewLegConfined": "new-leg never rewrites: it creates each file O_CREATE|O_EXCL inside an os.Root after a preflight that refuses any existing path, and on failure removes only what this run created",
}

// publisherBypasses answers one line per file operation in src that changes
// an entry outside the publisher: "file: function: what".
func publisherBypasses(t *testing.T, rel string, src []byte) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
	// the names the os package goes by in this file, and every other import's
	osNames, pkgNames := map[string]bool{}, map[string]bool{}
	var found []string
	for _, imp := range file.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		name := path[strings.LastIndex(path, "/")+1:]
		if imp.Name != nil {
			name = imp.Name.Name
		}
		if path == "os" || path == "io/ioutil" {
			if name == "." {
				found = append(found, fmt.Sprintf("%s: imports %s as \".\": its file operations cannot be told from the package's own", rel, path))
			}
			osNames[name] = true
			continue
		}
		pkgNames[name] = true
	}
	for _, decl := range file.Decls {
		fn := "(package level)"
		if fd, ok := decl.(*ast.FuncDecl); ok {
			fn = fd.Name.Name
		}
		readOnly := map[*ast.SelectorExpr]bool{}
		ast.Inspect(decl, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				sel, ok := n.Fun.(*ast.SelectorExpr)
				if !ok || !publisherOSFuncs[sel.Sel.Name] {
					return true
				}
				// OpenFile(name, os.O_RDONLY, perm) reads
				if sel.Sel.Name == "OpenFile" && len(n.Args) == 3 {
					if flag, ok := n.Args[1].(*ast.SelectorExpr); ok && flag.Sel.Name == "O_RDONLY" {
						if x, ok := flag.X.(*ast.Ident); ok && osNames[x.Name] {
							readOnly[sel] = true
							return true
						}
					}
				}
				if x, ok := sel.X.(*ast.Ident); ok && (osNames[x.Name] || pkgNames[x.Name]) {
					return true // a package's function: os is caught below, any other package is not os
				}
				found = append(found, fmt.Sprintf("%s: %s: calls the method %s at line %d", rel, fn, sel.Sel.Name, fset.Position(n.Pos()).Line))
			case *ast.SelectorExpr:
				if x, ok := n.X.(*ast.Ident); ok && osNames[x.Name] && publisherOSFuncs[n.Sel.Name] && !readOnly[n] {
					found = append(found, fmt.Sprintf("%s: %s: uses %s.%s at line %d", rel, fn, x.Name, n.Sel.Name, fset.Position(n.Pos()).Line))
				}
			}
			return true
		})
	}
	return found
}

// cliPackages answers the directories, relative to root, of cmd/schema and
// every package of this module it links, by following the imports of their
// non-test files.
func cliPackages(t *testing.T, root string) []string {
	t.Helper()
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	var module string
	for line := range strings.SplitSeq(string(mod), "\n") {
		if rest, ok := strings.CutPrefix(line, "module "); ok {
			module = strings.TrimSpace(rest)
			break
		}
	}
	if module == "" {
		t.Fatal("go.mod has no module line")
	}
	seen := map[string]bool{}
	queue := []string{"cmd/schema"}
	for len(queue) > 0 {
		dir := queue[0]
		queue = queue[1:]
		if seen[dir] {
			continue
		}
		seen[dir] = true
		for _, path := range goSources(t, filepath.Join(root, dir)) {
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range file.Imports {
				p, _ := strconv.Unquote(imp.Path.Value)
				if rest, ok := strings.CutPrefix(p, module+"/"); ok {
					queue = append(queue, rest)
				}
			}
		}
	}
	dirs := make([]string, 0, len(seen))
	for dir := range seen {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	return dirs
}

// goSources is the non-test Go files of one directory.
func goSources(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if name := e.Name(); !e.IsDir() && strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			out = append(out, filepath.Join(dir, name))
		}
	}
	return out
}

func TestCLIWritesGoThroughThePublisher(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	scanned := map[string]bool{}
	used := map[string]bool{}
	var bypasses []string
	for _, dir := range cliPackages(t, root) {
		if dir == "internal/atomicfile" {
			continue // the publisher itself
		}
		for _, path := range goSources(t, filepath.Join(root, dir)) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				t.Fatal(err)
			}
			rel = filepath.ToSlash(rel)
			scanned[rel] = true
			for _, b := range publisherBypasses(t, rel, src) {
				parts := strings.SplitN(b, ": ", 3)
				if key := parts[0] + ": " + parts[1]; len(parts) == 3 && publisherAllowed[key] != "" {
					used[key] = true
					continue
				}
				bypasses = append(bypasses, b)
			}
		}
	}
	if len(scanned) == 0 {
		t.Fatal("the gate scanned no files: it would pass over an empty set")
	}
	// the files that held the writes this gate was written for are in the scan
	for _, must := range []string{
		"cmd/schema/main.go", "cmd/schema/pin.go", "compiler/compiler.go",
		"internal/baseline/file.go", "internal/lockfile/file.go", "internal/lockfile/lineage.go",
		"internal/lockfile/writer_import.go", "internal/tablepack/unpack.go",
	} {
		if !scanned[must] {
			t.Errorf("%s was not scanned (%d files were): the walk from cmd/schema no longer reaches it", must, len(scanned))
		}
	}
	for _, b := range bypasses {
		t.Errorf("%s\n\ta schema command changes a file only through internal/atomicfile (WriteFile, or a Txn for several entries), so a failed write leaves the file as it was; a function that truly cannot goes in publisherAllowed with its reason, and cmd/schema's TestNoCommandDamagesAFileItRewrites gets a row for the command", b)
	}
	for key := range publisherAllowed {
		if !used[key] {
			t.Errorf("publisherAllowed lists %q, which changes no entry directly any more: remove it", key)
		}
	}
}

// TestCLIPublisherGateNegativeControl holds the gate to what it must refuse
// and what it must let through, on sources written here.
func TestCLIPublisherGateNegativeControl(t *testing.T) {
	t.Parallel()
	const head = "package p\n\nimport (\n\t\"os\"\n\tstdos \"os\"\n\t\"example.com/m/internal/atomicfile\"\n)\n\nvar _ = stdos.Getpid\nvar _ = atomicfile.New\n\n"
	refused := map[string]string{
		"os.WriteFile":             "func f(p string, b []byte) error { return os.WriteFile(p, b, 0o644) }",
		"os.Create":                "func f(p string) { _, _ = os.Create(p) }",
		"os.OpenFile for writing":  "func f(p string) { _, _ = os.OpenFile(p, os.O_WRONLY|os.O_TRUNC, 0o644) }",
		"os.OpenFile, flags a var": "func f(p string, flags int) { _, _ = os.OpenFile(p, flags, 0o644) }",
		"os.Rename":                "func f(a, b string) error { return os.Rename(a, b) }",
		"os.Remove":                "func f(a string) error { return os.Remove(a) }",
		"os.CreateTemp":            "func f(d string) { _, _ = os.CreateTemp(d, \"x\") }",
		"os under another name":    "func f(p string, b []byte) error { return stdos.WriteFile(p, b, 0o644) }",
		"os.WriteFile as a value":  "func f() func(string, []byte, os.FileMode) error { return os.WriteFile }",
		"a root's WriteFile":       "func f(r *os.Root, b []byte) error { return r.WriteFile(\"a\", b, 0o644) }",
		"a root's OpenFile":        "func f(r *os.Root) { _, _ = r.OpenFile(\"a\", os.O_CREATE|os.O_WRONLY, 0o644) }",
		"a root's RemoveAll":       "func f(r *os.Root) error { return r.RemoveAll(\"a\") }",
		"a file's Truncate":        "func f(x *os.File) error { return x.Truncate(0) }",
		"a write at package level": "var _ = os.WriteFile(\"a\", nil, 0o644)",
	}
	for name, body := range refused {
		if got := publisherBypasses(t, "p/p.go", []byte(head+body+"\n")); len(got) == 0 {
			t.Errorf("%s was let through:\n%s", name, body)
		}
	}
	allowed := map[string]string{
		"the publisher":             "func f(p string, b []byte) error { return atomicfile.WriteFile(p, b, 0o644) }",
		"a transaction":             "func f(p string, b []byte) error { t := atomicfile.New(); t.Write(p, b, 0o644); t.Delete(p); return t.Commit() }",
		"reading":                   "func f(p string) { _, _ = os.ReadFile(p); _, _ = os.Open(p); _, _ = os.Stat(p); _, _ = os.ReadDir(p) }",
		"os.OpenFile read-only":     "func f(p string) { _, _ = os.OpenFile(p, os.O_RDONLY, 0) }",
		"a root opened and read":    "func f(p string) { r, _ := os.OpenRoot(p); _, _ = r.Lstat(\"a\"); _, _ = r.ReadFile(\"a\") }",
		"making a directory":        "func f(p string) error { return os.MkdirAll(p, 0o755) }",
		"writing to standard error": "func f() { _, _ = os.Stderr.Write(nil); _, _ = os.Stdout.WriteString(\"\") }",
	}
	for name, body := range allowed {
		if got := publisherBypasses(t, "p/p.go", []byte(head+body+"\n")); len(got) != 0 {
			t.Errorf("%s was refused: %v\n%s", name, got, body)
		}
	}
}
