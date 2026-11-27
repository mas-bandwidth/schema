package ci

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/compiler"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/python/pyrun"
)

// docs/PORTING.md I12 for Python: every Python fragment in docs/USAGE.md is
// marked `<!-- python-usage <unit> -->` ... `<!-- /python-usage -->` around one
// ```python fence, where <unit> is the corpus directory the fragment is
// written against. The gate generates that unit's package with the compiler,
// puts the fragment beside it as main.py (after pyrun.ImportPrelude, the one
// sys.path line an isolated interpreter needs) and runs it under both pinned
// interpreters, so the documented surface goes red with the code rather than
// drifting from it. A fragment checks its own results and exits non-zero when
// one is wrong.

// pythonUsageMarker opens a fragment and names its unit.
var pythonUsageMarker = regexp.MustCompile(`<!-- python-usage ([A-Za-z0-9_./-]+) -->`)

type pythonUsageFragment struct {
	unit   string // the corpus directory, relative to the repository root
	source string // the fenced Python, verbatim
}

// pythonUsageFragments extracts every marked Python fragment from page, in
// page order. A marker without its closing marker or its fence is an error,
// never a skipped fragment.
func pythonUsageFragments(t *testing.T, page string) []pythonUsageFragment {
	t.Helper()
	var out []pythonUsageFragment
	rest := page
	for {
		loc := pythonUsageMarker.FindStringSubmatchIndex(rest)
		if loc == nil {
			break
		}
		unit := rest[loc[2]:loc[3]]
		block, after, ok := strings.Cut(rest[loc[1]:], "<!-- /python-usage -->")
		if !ok {
			t.Fatalf("the python-usage fragment for %s has no closing <!-- /python-usage --> marker", unit)
		}
		_, source, ok := strings.Cut(block, "```python\n")
		if !ok {
			t.Fatalf("the python-usage fragment for %s has no ```python fence", unit)
		}
		source, _, ok = strings.Cut(source, "```")
		if !ok {
			t.Fatalf("the python-usage fragment for %s has an unclosed fence", unit)
		}
		out = append(out, pythonUsageFragment{unit: unit, source: source})
		rest = after
	}
	return out
}

// pythonUsageUnits generates the Python package of every unit the fragments
// name, once per unit, keyed by the unit's directory.
func pythonUsageUnits(t *testing.T, root string, fragments []pythonUsageFragment) map[string]map[string][]byte {
	t.Helper()
	c := compiler.New()
	units := map[string]map[string][]byte{}
	for _, f := range fragments {
		if _, ok := units[f.unit]; ok {
			continue
		}
		dir := filepath.Join(root, filepath.FromSlash(f.unit))
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Fatalf("a python-usage fragment names %s, which is not a directory of the repository", f.unit)
		}
		paths, err := compiler.GatherPaths([]string{dir})
		if err != nil {
			t.Fatalf("%s: %v", f.unit, err)
		}
		u, err := c.Load(paths)
		if err != nil {
			t.Fatalf("%s does not compile: %v", f.unit, err)
		}
		files, err := c.Generate(u, "python", nil)
		if err != nil {
			t.Fatalf("%s: schema generate --lang python refused it: %v", f.unit, err)
		}
		units[f.unit] = files
	}
	return units
}

// pythonUsageTree writes a generated package and the fragment as main.py
// into a fresh directory.
func pythonUsageTree(t *testing.T, files map[string][]byte, source string) string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.py"), []byte(pyrun.ImportPrelude+source), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// pythonUsageCorpus reads docs/USAGE.md, holds it to carrying a packet and a
// table fragment, and generates every unit they name.
func pythonUsageCorpus(t *testing.T) ([]pythonUsageFragment, map[string]map[string][]byte) {
	t.Helper()
	root := repoRoot(t)
	page, err := os.ReadFile(filepath.Join(root, "docs", "USAGE.md"))
	if err != nil {
		t.Fatal(err)
	}
	fragments := pythonUsageFragments(t, string(page))
	if len(fragments) == 0 {
		t.Fatal("docs/USAGE.md carries no <!-- python-usage <unit> --> fragment: the Python leg's documented surface is untested")
	}
	var packet, table bool
	for _, f := range fragments {
		packet = packet || strings.Contains(f.source, ".write()")
		table = table || strings.Contains(f.source, ".save()")
	}
	if !packet || !table {
		t.Fatalf("docs/USAGE.md's Python fragments must show a packet write and a table save; packet=%v table=%v", packet, table)
	}
	return fragments, pythonUsageUnits(t, root, fragments)
}

func TestPythonUsageFragmentsRun(t *testing.T) {
	t.Parallel()
	fragments, units := pythonUsageCorpus(t)
	for i, f := range fragments {
		for _, floor := range []bool{false, true} {
			name := filepath.Base(f.unit) + "-" + pyrun.CurrentVersion
			if floor {
				name = filepath.Base(f.unit) + "-" + pyrun.FloorVersion
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				dir := pythonUsageTree(t, units[f.unit], f.source)
				if floor {
					pyrun.RunFloor(t, dir, "main.py", nil)
				} else {
					pyrun.Run(t, dir, "main.py", nil)
				}
				t.Logf("docs/USAGE.md Python fragment %d (%s) ran", i+1, f.unit)
			})
		}
	}
}

// TestPythonUsageFragmentsRunNegativeControl plants one fault at a time in a
// copy of the generated package a fragment runs against, and requires that
// fragment to fail naming it: a documented method renamed out from under the
// page in the generated module, and the runtime's Refused renamed in its
// _schema package.
func TestPythonUsageFragmentsRunNegativeControl(t *testing.T) {
	t.Parallel()
	fragments, units := pythonUsageCorpus(t)
	controls := []struct {
		name, file, from, to, want string
	}{
		{"packet-write-renamed", "/Types.py", "    def write(self) -> bytes:\n", "    def write_renamed(self) -> bytes:\n", "'write'"},
		{"table-save-renamed", "/Graph.py", "    def save(self) -> bytes:\n", "    def save_renamed(self) -> bytes:\n", "'save'"},
		{"table-load-renamed", "/Graph.py", "    def load(cls, data:", "    def load_renamed(cls, data:", "'load'"},
		{"runtime-refused-renamed", "/_schema/__init__.py", "class Refused(ValueError):\n", "class Refusal(ValueError):\n", "'Refused'"},
	}
	for _, c := range controls {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			planted := 0
			for _, f := range fragments {
				files := map[string][]byte{}
				hit := false
				for name, data := range units[f.unit] {
					if strings.HasSuffix(name, c.file) && strings.Contains(string(data), c.from) {
						data = []byte(strings.ReplaceAll(string(data), c.from, c.to))
						hit = true
					}
					files[name] = data
				}
				if !hit || !strings.Contains(f.source, strings.Trim(c.want, "'")) {
					continue
				}
				planted++
				dir := pythonUsageTree(t, files, f.source)
				stdout, stderr, err := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", nil)
				if err == nil {
					t.Fatalf("NEGATIVE CONTROL FAILED: with %s planted the %s fragment still ran\nstdout:\n%s", c.name, f.unit, stdout)
				}
				if !strings.Contains(stderr+stdout, c.want) {
					t.Fatalf("with %s planted the %s fragment failed without naming %s:\nstderr:\n%s\nstdout:\n%s", c.name, f.unit, c.want, stderr, stdout)
				}
				t.Logf("%s: the %s fragment fails naming %s", c.name, f.unit, c.want)
			}
			if planted == 0 {
				t.Fatalf("NEGATIVE CONTROL: %s planted into no fragment's package (%q not found in a generated %s)", c.name, c.from, c.file)
			}
		})
	}
}
