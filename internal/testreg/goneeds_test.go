package testreg

import (
	"encoding/json"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// builtReads is each build output a Go test opens by path and does not make
// itself, and the make target that writes it.
var builtReads = map[string]string{
	"fixedform-corpus": "tables-fixedform-corpus",
}

// builtJoinReads is each build output a Go test opens only as
// filepath.Join(.., "build", frag, ...), and the make target that writes it.
// Its repo-relative spelling ("build/" + frag) is no read here: it is how
// internal/ci's tests quote Makefile text, how tools/schema-test names a make
// argument it builds itself, and how the harness hands a driver a path at run
// time. Undeclared, each red its test the first time it ran alone (the dart
// cook-write rows, the elixir message conformance, the luau wire-fuzz pipe).
// A test that also makes the output itself (csConformanceBuild) is held too:
// the declared build runs before its call, so the call is the test's time.
var builtJoinReads = map[string]string{
	"tables-generated-dart":         "build/tables-generated-dart/.stamp",
	"tables-generated-dart-framing": "build/tables-generated-dart-framing/countedmin/.stamp",
	"elixir-tables-ebin":            "build-conformance-elixir",
	"luaufuzzhost":                  "build/luaufuzzhost/luaufuzzhost",
	"conformance-harness":           "build/conformance-harness",
}

// TestEveryBuiltReadIsDeclared: a package whose Go tests open a build output
// by path has a GoNeedsPath line naming the target that writes it, and a line
// whose tests are named reads it from one of them. A package reads an output
// when any of its files names it by path (a helper beside its tests counts).
// A helper package that reads it for its callers (internal/fixedproof) is not
// followed: importing one is not reading through it (internal/ci takes only
// its case list), so its callers are held by their own lines. Undeclared, the read passed or failed by
// which unit had run in the checkout before it (fullrun ab1e1e195a:
// javatable red "no fixed-form corpus", the other corpus readers green
// behind a unit that happened to build it).
func TestEveryBuiltReadIsDeclared(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..")
	needs, err := LoadGoNeeds(filepath.Join(root, GoNeedsPath))
	if err != nil {
		t.Fatal(err)
	}
	declared := map[string]bool{} // which of its tests read it is the line's to say
	for _, n := range needs {
		declared[n.Package+" "+n.Target] = true
	}
	self := filepath.Join("internal", "testreg", "goneeds_test.go")
	type pkgInfo struct {
		reads    map[string]string // target -> a file that names its output
		hasTests bool
	}
	pkgs := map[string]*pkgInfo{} // "./dir"
	info := func(pkg string) *pkgInfo {
		if pkgs[pkg] == nil {
			pkgs[pkg] = &pkgInfo{reads: map[string]string{}}
		}
		return pkgs[pkg]
	}
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch rel {
			case "build", "dist", "generated", ".git", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || rel == self {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		pkg := "./" + filepath.ToSlash(filepath.Dir(rel))
		p := info(pkg)
		if strings.HasSuffix(path, "_test.go") {
			p.hasTests = true
		}
		lits := stringLiterals(src)
		for i, lit := range lits {
			prev := ""
			if i > 0 {
				prev = lits[i-1]
			}
			for frag, target := range builtReads {
				if readsBuilt(prev, lit, frag) {
					p.reads[target] = rel
				}
			}
			for frag, target := range builtJoinReads {
				if prev == "build" && lit == frag {
					p.reads[target] = rel
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{} // "pkg target" -> why it reads it
	for pkg, p := range pkgs {
		if !p.hasTests {
			continue
		}
		for target, file := range p.reads {
			want[pkg+" "+target] = file + " reads its output"
		}
	}
	if len(want) == 0 {
		t.Fatal("found no Go test that reads a build output: the scan reads nothing")
	}
	var missing []string
	for k, why := range want {
		if !declared[k] {
			pkg, target, _ := strings.Cut(k, " ")
			missing = append(missing, "needs "+pkg+" <its tests> "+target+"   ("+why+")")
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		// one line, so a run's first line of the failure names each missing
		// declaration (a plant matches its reason there)
		t.Fatalf("%s does not declare what these Go tests read: %s", GoNeedsPath, strings.Join(missing, "; "))
	}
}

// readsBuilt reports whether a string literal names the build output frag as
// a path a test opens: the element after "build" (filepath.Join(..,
// "build", frag)), or a relative path to it or into it
// ("../../../build/" + frag, "../../build/" + frag + "/manifest.txt"). A
// literal that only mentions it inside other text (a workflow line, a fake
// script) or under another directory is not a read.
func readsBuilt(prev, lit, frag string) bool {
	if lit == frag {
		return prev == "build"
	}
	p := strings.TrimSuffix(lit, "/")
	for strings.HasPrefix(p, "../") {
		p = p[len("../"):]
	}
	return p == "build/"+frag || strings.HasPrefix(p, "build/"+frag+"/")
}

// stringLiterals is every string literal in a Go file, between its quotes.
func stringLiterals(src []byte) []string {
	var s scanner.Scanner
	fset := token.NewFileSet()
	s.Init(fset.AddFile("", fset.Base(), len(src)), src, nil, 0)
	var out []string
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			return out
		}
		if tok == token.STRING {
			out = append(out, strings.Trim(lit, "\"`"))
		}
	}
}

// TestGoNeedsSelectByPackageAndWholeTestName: a needs line applies to the
// tests of its package its regexp matches whole, and to nothing else; a
// malformed line is refused by its line number.
func TestGoNeedsSelectByPackageAndWholeTestName(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "needs.txt")
	if err := os.WriteFile(path, []byte("# c\nneeds ./internal/x TestA corpus\nneeds ./internal/y .* other\nneeds ./internal/y TestA corpus\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	needs, err := LoadGoNeeds(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ pkg, test, want string }{
		{"./internal/x", "TestA", "corpus"},
		{"./internal/x", "TestAB", ""},
		{"./internal/x", "TestB", ""},
		{"./internal/y", "TestA", "other,corpus"},
		{"./internal/z", "TestA", ""},
	} {
		if got := strings.Join(GoNeedsOf(needs, c.pkg, c.test), ","); got != c.want {
			t.Errorf("%s %s: %q, want %q", c.pkg, c.test, got, c.want)
		}
	}
	for _, bad := range []string{"needs internal/x TestA corpus\n", "needs ./internal/x TestA\n", "needs ./internal/x ( corpus\n"} {
		if err := os.WriteFile(path, []byte(bad), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadGoNeeds(path); err == nil || !strings.Contains(err.Error(), ":1:") {
			t.Errorf("%q: %v, want a refusal naming line 1", bad, err)
		}
	}
}

// TestEveryModuleBuiltAgainstBuildIsDeclared: a nested Go module whose go.mod
// replaces an import with a directory under build/ compiles only once that
// tree is written, so every package of it that holds tests has a
// GoNeedsPath line. Undeclared, fullrun's go -C run failed at setup
// ("replacement directory ../../../build/tables-generated-go/block does not
// exist": 69 reds in fullrun e38eafd02b retest-1).
func TestEveryModuleBuiltAgainstBuildIsDeclared(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..")
	needs, err := LoadGoNeeds(filepath.Join(root, GoNeedsPath))
	if err != nil {
		t.Fatal(err)
	}
	declared := map[string]bool{}
	for _, n := range needs {
		declared[n.Package] = true
	}
	intoBuild := regexp.MustCompile(`(?m)^\s*replace\s+\S+(\s+\S+)?\s+=>\s+(\.\./)+build/`)
	var modules []string // module dirs, relative to root
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch rel {
			case "build", "dist", "generated", ".git", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "go.mod" || rel == "go.mod" {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if intoBuild.Match(src) {
			modules = append(modules, filepath.Dir(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(modules) == 0 {
		t.Fatal("found no nested module built against build/: the scan reads nothing")
	}
	var missing []string
	for _, m := range modules {
		err := filepath.WalkDir(filepath.Join(root, m), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if path != filepath.Join(root, m) {
					if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
						return filepath.SkipDir // a module of its own, scanned by itself
					}
				}
				return nil
			}
			if !strings.HasSuffix(d.Name(), "_test.go") {
				return nil
			}
			rel, err := filepath.Rel(root, filepath.Dir(path))
			if err != nil {
				return err
			}
			pkg := "./" + filepath.ToSlash(rel)
			if !declared[pkg] {
				missing = append(missing, pkg+" (module "+filepath.ToSlash(m)+")")
				declared[pkg] = true // once
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("%s declares no build for these packages of modules built against build/:\n%s", GoNeedsPath, strings.Join(missing, "\n"))
	}
}

// TestNoRowMakesATargetTheNeedsTableDeclares: a gotest row's own "make" input is
// built as one step, so a target that go-needs.txt declares as a chain of
// steps (the generated tree, the driver) is never made through it: the row
// would build the whole chain inside one step's limit. A row names what it
// builds in go-needs.txt. Planted: the json-walk row with its old
// "make": ".stamp" input is named.
func TestNoRowMakesATargetTheNeedsTableDeclares(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..")
	needs, err := LoadGoNeeds(filepath.Join(root, GoNeedsPath))
	if err != nil {
		t.Fatal(err)
	}
	declared := map[string]bool{}
	for _, n := range needs {
		declared[n.Target] = true
	}
	reg, err := Load(filepath.Join(root, DefaultPath))
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range reg.Tests {
		if row.Adapter != "gotest" {
			continue
		}
		var in struct{ Make string }
		if json.Unmarshal(row.Inputs, &in) != nil || in.Make == "" {
			continue
		}
		if declared[in.Make] {
			t.Errorf("%s makes %s in its own step, and %s declares it as a step of its own: declare the row's builds there", row.Key, in.Make, GoNeedsPath)
		}
	}
	if len(needs) == 0 || len(reg.Tests) == 0 {
		t.Fatal("the needs table or the registry is empty; the check holds nothing")
	}
}
