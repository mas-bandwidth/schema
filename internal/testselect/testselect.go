// Package testselect names a Go package's top-level tests for a CI row that
// runs what the package's other rows do not: a complement chosen at the root,
// by name, never by a skip (Glenn, 2026-11-25: "NO SKIPS"; "if you don't want
// to run a test, don't run it (i mean at the root)").
//
// A row that once read `go test -skip '<the other rows>' ./pkg` now reads
// `go test -run "$(go run ./tools/gotestselect -not '<the other rows>' ./pkg)" ./pkg`:
// the same tests, listed. The selection also leaves out every test whose tier
// is nightly (a tier line in test/registry/unmoved.tsv, or a registry row), so
// a per-change row never runs a nightly test; the nightly lane selects them.
package testselect

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// Names are the top-level tests, fuzz targets and examples `go test` would
// run in the package in dir under ctx (its build constraints), sorted.
func Names(ctx build.Context, dir string) ([]string, error) {
	pkg, err := ctx.ImportDir(dir, 0)
	if err != nil {
		var none *build.NoGoError
		if !errors.As(err, &none) {
			return nil, err
		}
	}
	var files []string
	if pkg != nil {
		files = append(append(files, pkg.TestGoFiles...), pkg.XTestGoFiles...)
	}
	fset := token.NewFileSet()
	var names []string
	for _, f := range files {
		file, err := parser.ParseFile(fset, filepath.Join(dir, f), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name.Name == "TestMain" {
				continue
			}
			for _, p := range []string{"Test", "Fuzz", "Example"} {
				if isTestName(fn.Name.Name, p) {
					names = append(names, fn.Name.Name)
				}
			}
		}
	}
	sort.Strings(names)
	return names, nil
}

// isTestName is go test's rule: the prefix, then nothing or a character that
// is not a lower-case letter.
func isTestName(name, prefix string) bool {
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	rest := name[len(prefix):]
	return rest == "" || rest[0] < 'a' || rest[0] > 'z'
}

// Nightly is every Go test whose tier is nightly, as "<dir>:<Name>" with dir
// slash-separated and relative to root: the tier lines of
// test/registry/unmoved.tsv, the gotest rows of test/registry/registry.jsonl,
// and its per-language rows whose test is named with testreg.LangPlaceholder
// (tools/namebuild's TestNameBuildability_{lang}), one test per language the
// registry lists.
func Nightly(root string) (map[string]bool, error) {
	out := map[string]bool{}
	ledger, err := os.ReadFile(filepath.Join(root, "test", "registry", "unmoved.tsv"))
	if err != nil {
		return nil, err
	}
	for line := range strings.SplitSeq(string(ledger), "\n") {
		f := strings.Split(line, "\t")
		if len(f) >= 5 && f[0] == "tier" && strings.HasPrefix(f[4], "nightly:") {
			out[f[1]] = true
		}
	}
	reg, err := os.Open(filepath.Join(root, "test", "registry", "registry.jsonl"))
	if err != nil {
		return nil, err
	}
	defer reg.Close() //nolint:errcheck // read-only
	rs := bufio.NewScanner(reg)
	rs.Buffer(make([]byte, 1<<20), 1<<20)
	var langs, perLang []string
	for n := 1; rs.Scan(); n++ {
		var row struct {
			Rec, Tier, Adapter, Scope, Name string
			Inputs                          struct{ Package, Test string }
		}
		if err := json.Unmarshal(rs.Bytes(), &row); err != nil {
			return nil, fmt.Errorf("registry.jsonl:%d: %w", n, err)
		}
		place := strings.TrimPrefix(row.Inputs.Package, "./") + ":" + row.Inputs.Test
		switch {
		case row.Rec == "lang" && row.Name != testreg.LangFree:
			langs = append(langs, row.Name)
		case row.Rec != "test" || row.Tier != "nightly" || row.Inputs.Test == "":
		case row.Adapter == "gotest":
			out[place] = true
		case row.Scope == "" && row.Inputs.Package != "" && strings.Contains(row.Inputs.Test, testreg.LangPlaceholder):
			perLang = append(perLang, place)
		}
	}
	for _, p := range perLang {
		for _, l := range langs {
			out[strings.ReplaceAll(p, testreg.LangPlaceholder, l)] = true
		}
	}
	return out, rs.Err()
}

// Select is the -run pattern naming every test of the packages in dirs that
// run matches (nil matches all) and not does not (nil matches none) and,
// unless all, whose tier is not nightly. An empty selection is an error: a row that selects nothing runs
// nothing and must not pass.
func Select(ctx build.Context, root string, dirs []string, run, not *regexp.Regexp, all bool) (string, error) {
	nightly := map[string]bool{}
	if !all {
		var err error
		if nightly, err = Nightly(root); err != nil {
			return "", err
		}
	}
	seen := map[string]bool{}
	left := map[string]string{} // a name left out as nightly, and where
	var keep []string
	for _, dir := range dirs {
		rel, err := filepath.Rel(root, dir)
		if err != nil {
			return "", err
		}
		names, err := Names(ctx, dir)
		if err != nil {
			return "", err
		}
		for _, n := range names {
			if (run != nil && !run.MatchString(n)) || (not != nil && not.MatchString(n)) {
				continue
			}
			if nightly[filepath.ToSlash(rel)+":"+n] {
				left[n] = filepath.ToSlash(rel)
				continue
			}
			if !seen[n] {
				seen[n] = true
				keep = append(keep, n)
			}
		}
	}
	// the pattern is names only, and go test applies it to every package:
	// a name nightly in one package and kept in another cannot be said
	for n, where := range left {
		if seen[n] {
			return "", fmt.Errorf("%s is nightly in %s and not in another package of the selection; select those packages apart", n, where)
		}
	}
	if len(keep) == 0 {
		return "", fmt.Errorf("the selection over %s names no test", strings.Join(dirs, " "))
	}
	sort.Strings(keep)
	return "^(" + strings.Join(keep, "|") + ")$", nil
}
