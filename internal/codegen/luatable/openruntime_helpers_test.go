package luatable

import (
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// THE OPEN-RUNTIME GATES' SHARED HELPERS (the port/lua/open/* entries of
// docs/roadmap.sexp). A gate builds one manifest unit, optionally sabotages the
// emitted runtime in memory (the negative controls: a plant is a textual edit
// of a copy, never of the source), and runs a Lua probe over it.

const manifestRel = "testdata/conformance/tables/MANIFEST.txt"

// builtUnit is one manifest unit generated: its table module's basename and
// the emitted files, ready to be patched and written.
type builtUnit struct {
	key    string
	module string
	files  map[string][]byte
}

// buildManifestUnit generates the manifest unit key.
func buildManifestUnit(t *testing.T, key string) builtUnit {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	paths, ok := manifestUnits(t, filepath.Join(repo, manifestRel))[key]
	if !ok {
		t.Fatalf("the manifest names no unit %s", key)
	}
	var full []string
	for _, p := range paths {
		full = append(full, filepath.Join(repo, p))
	}
	u := luarun.LoadUnit(t, full...)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("unit %s: %v", key, err)
	}
	return builtUnit{key: key, module: ModuleName(u), files: files}
}

// patched answers a copy of the unit's files with one runtime file edited:
// from must occur (a plant that matched nothing proves nothing) and every
// occurrence is replaced.
func (b builtUnit) patched(t *testing.T, file, from, to string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	maps.Copy(out, b.files)
	src, ok := out[file]
	if !ok {
		t.Fatalf("the emitted unit has no %s", file)
	}
	if !strings.Contains(string(src), from) {
		t.Fatalf("the plant's anchor is absent from %s: %q", file, from)
	}
	out[file] = []byte(strings.ReplaceAll(string(src), from, to))
	return out
}

// try writes files and the probe main into a fresh directory and runs it,
// answering what Try answers: a probe that is expected to fail does not fail
// the test.
func try(t *testing.T, files map[string][]byte, main string, inputs map[string][]byte) (string, string, error) {
	t.Helper()
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	return luarun.Try(t, dir, "main.lua", inputs)
}

// instanceWires reads the wire file of every instance row of a unit, in name
// order, as the probe's inputs w1..wN and the root of each.
func instanceWires(t *testing.T, unit string) (inputs map[string][]byte, roots []string, cases []string) {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	inputs = map[string][]byte{}
	rows := manifestRows(t, filepath.Join(repo, manifestRel), "instance")
	sort.Slice(rows, func(i, j int) bool { return rows[i].caseName < rows[j].caseName })
	for _, r := range rows {
		if r.unit != unit {
			continue
		}
		data, err := os.ReadFile(filepath.Join(repo, r.wire))
		if err != nil {
			t.Fatalf("read %s: %v", r.wire, err)
		}
		roots = append(roots, r.root)
		cases = append(cases, r.caseName)
		inputs["w"+strconv.Itoa(len(roots))] = data
	}
	return inputs, roots, cases
}

// instanceUnits answers every unit key that has an instance row, sorted.
func instanceUnits(t *testing.T) []string {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	seen := map[string]bool{}
	var out []string
	for _, r := range manifestRows(t, filepath.Join(repo, manifestRel), "instance") {
		if !seen[r.unit] {
			seen[r.unit] = true
			out = append(out, r.unit)
		}
	}
	sort.Strings(out)
	return out
}
