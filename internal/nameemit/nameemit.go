// Package nameemit reads the committed corpus testdata/name-buildability-emit:
// units whose declared names a target's emitter once spelled into code that
// target's toolchain rejects (a type named uint8_t, memcpy or NULL, a
// package named Math). The rule they pin: every unit
// `schema check` accepts generates code that builds in every target, and where
// a target can express a name, its emitter does.
//
// Each case is a directory of schema files, and cases.tsv names the targets
// each case was red in, one row per case:
//
//	<case>	<target,target,...>	<what broke>
//
// Each target's own package carries the test that generates and builds them
// (behind slowtest.Gate): this package only lists the cases and generates one.
// Every case is one the checker accepts: a case it refuses fails, because a
// skipped case would stop proving anything and nobody would see it stop.
package nameemit

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
)

// Case is one row of cases.tsv.
type Case struct {
	Name    string   // the case directory
	Dir     string   // its absolute path
	Targets []string // the targets it was red in
	Note    string   // what broke
}

// Dir is the corpus directory's absolute path.
func Dir(t testing.TB) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("nameemit: no caller")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata", "name-buildability-emit")
}

// All is every row of cases.tsv, in file order.
func All(t testing.TB) []Case {
	t.Helper()
	root := Dir(t)
	data, err := os.ReadFile(filepath.Join(root, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var out []Case
	for i, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 3 {
			t.Fatalf("cases.tsv line %d: want 3 tab-separated columns, have %d", i+1, len(cols))
		}
		out = append(out, Case{
			Name:    cols[0],
			Dir:     filepath.Join(root, cols[0]),
			Targets: strings.Split(cols[1], ","),
			Note:    cols[2],
		})
	}
	return out
}

// For is the rows that name target.
func For(t testing.TB, target string) []Case {
	t.Helper()
	var out []Case
	for _, c := range All(t) {
		if slices.Contains(c.Targets, target) {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		t.Fatalf("cases.tsv names no case for target %s", target)
	}
	return out
}

// Generate loads the case and emits target through the public driver. It
// fails the test when the checker refuses the unit (a corpus case must be one
// the checker accepts) and when the target's emitter does.
func Generate(t testing.TB, c Case, target string) map[string][]byte {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(c.Dir, "*.schema"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		t.Fatalf("%s holds no schema file", c.Dir)
	}
	schema := compiler.New()
	u, err := schema.Load(paths)
	if err != nil {
		t.Fatalf("schema check refuses %s, so it proves nothing about the emitter: drop the case or rename it to a name the checker accepts: %v", c.Name, err)
	}
	files, err := schema.Generate(u, target, nil)
	if err != nil {
		t.Fatalf("%s: the %s emitter refused a unit the checker accepts: %v", c.Name, target, err)
	}
	return files
}

// Write puts every top-level file whose name ends in one of exts into dir and
// returns their names, sorted.
func Write(t testing.TB, files map[string][]byte, dir string, exts ...string) []string {
	t.Helper()
	var names []string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if !slices.ContainsFunc(exts, func(e string) bool { return strings.HasSuffix(name, e) }) {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
