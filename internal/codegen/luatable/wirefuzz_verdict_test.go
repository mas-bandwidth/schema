package luatable

// THE WIRE-FUZZ VERDICT PIPE FOR LUA (docs/SPEC-TABLES.md §4.2;
// test/conformance/README.md, "The wire-fuzz driver"): every mutant sent over
// the verdict pipe must produce the same strict read verdict in the Lua leg
// (test/conformance/lua/wirefuzz.lua) and the harness's Go oracle
// (internal/tablewire), and every accepted read must write back identical
// canonical bytes. This is the pipe half of the same claim the in-package
// differential (TestLuaWireFuzzAgreesWithTheGoEngine) makes with its own
// probes: it runs the REAL committed driver on its pipe through the REAL
// conformance wire-fuzz harness, the exact command `make tables-lua-wire-fuzz`
// runs, over a staged subset of the corpus that carries both fixed and variable
// roots.
//
// TestLuaWireFuzzVerdictPipe runs the pipe with zero divergences.
// TestLuaWireFuzzVerdictPipeNegativeControl plants a fault in the real emitted
// artifact (the generated load.lua) and requires the pipe to go red on it.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// luaFuzzPipeUnits is the subset of the corpus the verdict-pipe tests stage:
// backends (fixed-class roots) beside list/map/stream (variable-class roots),
// all with small wires so the enumerated pass stays quick.
var luaFuzzPipeUnits = []string{"backenddemo", "listdemo", "mapdemo", "streamdemo"}

// luaFuzzStagePipeUnits stages the named units into one generated directory
// (the shape test/conformance/lua/wirefuzz.lua reads), one subdirectory per
// unit key carrying the emitted module and its whole runtime. edit, when
// non-nil, patches the emitted load.lua of every unit (the negative control).
// It returns the directory and the staged groups.
func luaFuzzStagePipeUnits(t *testing.T, keys []string, edit func([]byte) []byte) (string, []*luaFuzzGroup) {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	manifestPath := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifestPath)
	byUnit := map[string][]manifestRow{}
	for _, row := range manifestRows(t, manifestPath, "instance") {
		byUnit[row.unit] = append(byUnit[row.unit], row)
	}
	dir := t.TempDir()
	var groups []*luaFuzzGroup
	for _, uname := range keys {
		paths, ok := units[uname]
		if !ok {
			continue
		}
		var full []string
		for _, p := range paths {
			full = append(full, filepath.Join(repo, p))
		}
		u := luarun.LoadUnit(t, full...)
		files, err := Generate(u)
		if err != nil {
			// A UNIT THE LUA TARGET REFUSES BY NAME is out of the differential,
			// exactly as build/conformance-lua names it refused.
			continue
		}
		if edit != nil {
			src, ok := files["load.lua"]
			if !ok {
				t.Fatal("the emitted unit has no load.lua")
			}
			files["load.lua"] = edit(src)
		}
		uDir := filepath.Join(dir, uname)
		if err := os.MkdirAll(uDir, 0o755); err != nil {
			t.Fatal(err)
		}
		luarun.WriteFiles(t, uDir, files)
		m := tabletext.NewModel(u)
		for _, row := range byUnit[uname] {
			g := &luaFuzzGroup{unit: uname, root: row.root, dir: uDir, m: m, def: m.Lookup(row.root)}
			if g.def == nil {
				t.Fatalf("unit %s declares no table %s", uname, row.root)
			}
			groups = append(groups, g)
		}
	}
	if len(groups) == 0 {
		t.Fatal("no staged unit generated a root")
	}
	return dir, groups
}

// luaFuzzHarness builds the conformance wire-fuzz harness once.
func luaFuzzHarness(t *testing.T) string {
	t.Helper()
	root := filepath.Join("..", "..", "..")
	harness := filepath.Join(t.TempDir(), "conformance-harness")
	build := exec.Command("go", "build", "-o", harness, "./test/conformance/harness")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build conformance-harness: %v\n%s", err, out)
	}
	return harness
}

// runLuaWireFuzzVerdictPipe runs the harness wire-fuzz verdict pipe over the
// staged directory. n is the random-pass size; expectGreen requires the run to
// pass. It returns the harness output.
func runLuaWireFuzzVerdictPipe(t *testing.T, generated string, n int, expectGreen bool) string {
	t.Helper()
	root := filepath.Join("..", "..", "..")
	harness := luaFuzzHarness(t)
	failed := filepath.Join(t.TempDir(), "failed.bin")
	driver := fmt.Sprintf("%s %s %s",
		luarun.Interpreter(t),
		filepath.Join("test", "conformance", "lua", "wirefuzz.lua"),
		generated)
	cmd := exec.Command(harness, "wire-fuzz",
		"--driver", driver,
		"--seed", "1",
		"--n", fmt.Sprintf("%d", n),
		"--failed", failed)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if expectGreen && err != nil {
		t.Fatalf("the verdict pipe failed:\n%s", out)
	}
	if !expectGreen && err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: the verdict pipe stayed green:\n%s", out)
	}
	return string(out)
}

func luaFuzzPipeCoverage(groups []*luaFuzzGroup) (fixed, variable int) {
	for _, g := range groups {
		if g.def.FixedDeclared {
			fixed++
		} else {
			variable++
		}
	}
	return fixed, variable
}

// TestLuaWireFuzzVerdictPipe is the verdict-pipe differential: the real driver
// on its pipe, through the real harness, over staged fixed and variable roots,
// with zero divergences (docs/SPEC-TABLES.md §4.2).
func TestLuaWireFuzzVerdictPipe(t *testing.T) {
	slowtest.Gate(t, luaFuzzCost+" over the verdict pipe (test/conformance/lua/wirefuzz.lua)")
	generated, groups := luaFuzzStagePipeUnits(t, luaFuzzPipeUnits, nil)
	fixed, variable := luaFuzzPipeCoverage(groups)
	if fixed == 0 || variable == 0 {
		t.Fatalf("the staged corpus covers %d fixed and %d variable roots; the gate must cover both classes", fixed, variable)
	}
	out := runLuaWireFuzzVerdictPipe(t, generated, 200, true)
	if !strings.Contains(out, "0 divergences") {
		t.Fatalf("the verdict pipe output lacks 0 divergences:\n%s", out)
	}
	if !regexp.MustCompile(`[1-9][0-9]* seeds over [1-9][0-9]* roots`).MatchString(out) {
		t.Fatalf("the verdict pipe fuzzed no live seed:\n%s", out)
	}
	if !regexp.MustCompile(`[1-9][0-9]* mutants`).MatchString(out) {
		t.Fatalf("the verdict pipe fuzzed no mutants:\n%s", out)
	}
}

// TestLuaWireFuzzVerdictPipeNegativeControl patches the emitted load in the
// staged artifact — one anchor, matched exactly once — so a string frame length
// past the body stops the body WITHOUT counting it malformed, and requires the
// verdict pipe to go red on that planted fault.
func TestLuaWireFuzzVerdictPipeNegativeControl(t *testing.T) {
	slowtest.Gate(t, luaFuzzCost+" over the verdict pipe negative control (test/conformance/lua/wirefuzz.lua)")
	sabotaged := strings.Replace(luaFuzzControlAnchor, "report.damaged = true\n", "", 1)
	edit := func(src []byte) []byte {
		if n := strings.Count(string(src), luaFuzzControlAnchor); n != 1 {
			t.Fatalf("NEGATIVE CONTROL FAILED: the sabotage anchor appears %d times in load.lua, want 1", n)
		}
		if n := strings.Count(string(src), luaWireCheckCall); n != 1 {
			t.Fatalf("NEGATIVE CONTROL FAILED: the wire check's call appears %d times in load.lua, want 1", n)
		}
		src = []byte(strings.Replace(string(src), luaWireCheckCall, luaWireCheckOff, 1))
		return []byte(strings.Replace(string(src), luaFuzzControlAnchor, sabotaged, 1))
	}
	generated, _ := luaFuzzStagePipeUnits(t, luaFuzzPipeUnits, edit)
	out := runLuaWireFuzzVerdictPipe(t, generated, 200, false)
	if !regexp.MustCompile(`FAILED after`).MatchString(out) {
		t.Fatalf("NEGATIVE CONTROL FAILED: the verdict pipe went red, but not on a divergence:\n%s", out)
	}
}
