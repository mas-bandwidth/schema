package testreg

import (
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// builtReads is each build output a Go test opens by path and does not make
// itself, and the make target that writes it.
var builtReads = map[string]string{
	"fixedform-corpus":         "tables-fixedform-corpus",
	"conformance-differential": "schema-squirrel-all-port-differential-harness",
}

// TestEveryBuiltReadIsDeclared: a package whose Go tests open a build output
// by path has a GoNeedsPath line naming the target that writes it. Undeclared,
// the read passed or failed by which unit had run in the checkout before it
// (fullrun ab1e1e195a: javatable red "no fixed-form corpus", the other corpus
// readers green behind a unit that happened to build it).
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
	want := map[string]string{} // "pkg target" -> a file that reads it
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
		if !strings.HasSuffix(path, "_test.go") || rel == self {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lits := stringLiterals(src)
		for i, lit := range lits {
			prev := ""
			if i > 0 {
				prev = lits[i-1]
			}
			for frag, target := range builtReads {
				if readsBuilt(prev, lit, frag) {
					want["./"+filepath.ToSlash(filepath.Dir(rel))+" "+target] = rel
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var missing []string
	for k, file := range want {
		if !declared[k] {
			pkg, target, _ := strings.Cut(k, " ")
			missing = append(missing, "needs "+pkg+" <its tests> "+target+"   ("+file+" reads its output)")
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("%s does not declare what these Go tests read:\n%s", GoNeedsPath, strings.Join(missing, "\n"))
	}
}

// readsBuilt reports whether a string literal names the build output frag as
// a path a test opens: the element after "build" (filepath.Join(..,
// "build", frag)), or a relative path to it ("../../../build/" + frag). A
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
	return p == "build/"+frag
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
		{"./internal/y", "TestA", "corpus,other"},
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
