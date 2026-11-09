package luautable_test

// THE WIRE-FUZZ VERDICT PIPE FOR LUAU (docs/SPEC-TABLES.md §4.2;
// test/conformance/README.md, "The wire-fuzz driver"): every mutant sent over
// the verdict pipe must produce the same read verdict in the Luau leg
// (test/conformance/luau/wirefuzz.luau under tools/luaufuzzhost) and the
// harness's Go oracle (internal/tablewire), and every accepted read must write
// back identical canonical bytes. This is the pipe half of the same claim the
// in-package differential (TestLuauWireFuzzAgreesWithTheGoEngine) makes with
// its own probes: it runs the REAL committed driver on its pipe through the
// REAL conformance wire-fuzz harness, the exact command `make tables-luau-wire-fuzz`
// runs, over a staged subset of the corpus that carries both fixed and variable
// roots.
//
// TestLuauWireFuzzVerdictPipe runs the pipe with zero divergences.
// TestLuauWireFuzzVerdictPipeNegativeControl plants a fault in the real emitted
// artifact (the generated schema_load.luau) and requires the pipe to go red on it.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// luauFuzzPipeUnits is the subset of the corpus the verdict-pipe tests stage:
// backenddemo (fixed-class roots) beside list/map/stream (variable-class roots),
// all with small wires so the enumerated pass stays quick.
var luauFuzzPipeUnits = []string{"backenddemo", "listdemo", "mapdemo", "streamdemo"}

// luauFuzzStagePipeUnits stages the named units into a temporary tree matching
// the layout test/conformance/luau/wirefuzz.luau resolves relative to itself
// (test/conformance/luau/wirefuzz.luau, build/luau-driver/units.luau, and
// build/conformance-luau/<unit>/). edit, when non-nil, patches the emitted
// schema_load.luau of every unit (the negative control). It returns the staged
// root directory and the staged groups.
func luauFuzzStagePipeUnits(t *testing.T, keys []string, edit func([]byte) []byte) (string, []*luauFuzzGroup) {
	t.Helper()
	units, _ := readManifest(t)
	byUnit := map[string][]reportRow{}
	for _, row := range manifestRows(t) {
		if row.kind == "instance" {
			byUnit[row.unit] = append(byUnit[row.unit], row)
		}
	}
	driverSrc, err := os.ReadFile(filepath.Join(root, "test", "conformance", "luau", "wirefuzz.luau"))
	if err != nil {
		t.Fatal(err)
	}
	stageDir := t.TempDir()
	driverDir := filepath.Join(stageDir, "test", "conformance", "luau")
	if err := os.MkdirAll(driverDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(driverDir, "wirefuzz.luau"), driverSrc, 0o644); err != nil {
		t.Fatal(err)
	}

	var unitsLuau strings.Builder
	unitsLuau.WriteString("return {\n")

	var groups []*luauFuzzGroup
	for _, uname := range keys {
		paths, ok := units[uname]
		if !ok {
			continue
		}
		schemaPaths, err := compiler.GatherPaths(prefix(paths))
		if err != nil {
			t.Fatalf("unit %s: %v", uname, err)
		}
		u, err := compiler.New().Load(schemaPaths)
		if err != nil {
			t.Fatalf("unit %s does not load: %v", uname, err)
		}
		files, err := luau.Generate(u)
		if err != nil {
			continue
		}
		tables, err := luautable.Generate(u)
		if err != nil {
			continue
		}
		maps.Copy(files, tables)
		if edit != nil {
			src, ok := files["schema_load.luau"]
			if !ok {
				t.Fatal("the emitted unit has no schema_load.luau")
			}
			files["schema_load.luau"] = edit(src)
		}
		uDir := filepath.Join(stageDir, "build", "conformance-luau", uname)
		if err := os.MkdirAll(uDir, 0o755); err != nil {
			t.Fatal(err)
		}
		var mods []string
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(uDir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
			if strings.HasSuffix(name, "Table.luau") {
				mods = append(mods, strings.TrimSuffix(name, ".luau"))
			}
		}
		sort.Strings(mods)
		fmt.Fprintf(&unitsLuau, "\t[%q] = {", uname)
		for i, mod := range mods {
			if i > 0 {
				unitsLuau.WriteString(",")
			}
			fmt.Fprintf(&unitsLuau, " %q", mod)
		}
		unitsLuau.WriteString(" },\n")

		m := tabletext.NewModel(u)
		seenRoot := map[string]bool{}
		for _, row := range byUnit[uname] {
			if seenRoot[row.root] {
				continue
			}
			seenRoot[row.root] = true
			g := &luauFuzzGroup{unit: uname, root: row.root, dir: uDir, m: m, def: m.Lookup(row.root)}
			if g.def == nil {
				t.Fatalf("unit %s declares no table %s", uname, row.root)
			}
			groups = append(groups, g)
		}
	}
	unitsLuau.WriteString("}\n")
	unitsDir := filepath.Join(stageDir, "build", "luau-driver")
	if err := os.MkdirAll(unitsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unitsDir, "units.luau"), []byte(unitsLuau.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if len(groups) == 0 {
		t.Fatal("no staged unit generated a root")
	}
	return stageDir, groups
}

var (
	luauFuzzHostOnce sync.Once
	luauFuzzHostPath string
	luauFuzzHostErr  error
)

// luauFuzzHost resolves or builds tools/luaufuzzhost once per test binary.
func luauFuzzHost(t *testing.T) string {
	t.Helper()
	if env := os.Getenv("LUAUFUZZHOST"); env != "" {
		if !filepath.IsAbs(env) {
			env = filepath.Join(root, env)
		}
		return env
	}
	luauFuzzHostOnce.Do(func() {
		rel := filepath.Join("build", "luaufuzzhost", "luaufuzzhost")
		full := filepath.Join(root, rel)
		if _, err := os.Stat(full); err != nil {
			cmd := exec.Command("make", rel)
			cmd.Dir = root
			if out, merr := cmd.CombinedOutput(); merr != nil {
				luauFuzzHostErr = fmt.Errorf("make %s: %w\n%s", rel, merr, out)
				return
			}
		}
		abs, err := filepath.Abs(full)
		if err != nil {
			luauFuzzHostErr = err
			return
		}
		luauFuzzHostPath = abs
	})
	if luauFuzzHostErr != nil {
		t.Fatal(luauFuzzHostErr)
	}
	return luauFuzzHostPath
}

// luauFuzzHarness builds the conformance wire-fuzz harness once.
func luauFuzzHarness(t *testing.T) string {
	t.Helper()
	harness := filepath.Join(t.TempDir(), "conformance-harness")
	build := exec.Command("go", "build", "-o", harness, "./test/conformance/harness")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build conformance-harness: %v\n%s", err, out)
	}
	return harness
}

// runLuauWireFuzzVerdictPipe runs the harness wire-fuzz verdict pipe over the
// staged directory. n is the random-pass size; expectGreen requires the run to
// pass. It returns the harness output.
func runLuauWireFuzzVerdictPipe(t *testing.T, stageDir string, n int, expectGreen bool) string {
	t.Helper()
	harness := luauFuzzHarness(t)
	host := luauFuzzHost(t)
	failed := filepath.Join(t.TempDir(), "failed.bin")
	driverScript := filepath.Join(stageDir, "test", "conformance", "luau", "wirefuzz.luau")
	driver := fmt.Sprintf("%s %s", host, driverScript)
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

func luauFuzzPipeCoverage(groups []*luauFuzzGroup) (fixed, variable int) {
	for _, g := range groups {
		if g.def.FixedDeclared {
			fixed++
		} else {
			variable++
		}
	}
	return fixed, variable
}

// TestLuauWireFuzzVerdictPipe is the verdict-pipe differential: the real driver
// on its pipe, through the real harness, over staged fixed and variable roots,
// with zero divergences (docs/SPEC-TABLES.md §4.2).
func TestLuauWireFuzzVerdictPipe(t *testing.T) {
	slowtest.Gate(t, luauFuzzCost+" over the verdict pipe (test/conformance/luau/wirefuzz.luau)")
	stageDir, groups := luauFuzzStagePipeUnits(t, luauFuzzPipeUnits, nil)
	fixed, variable := luauFuzzPipeCoverage(groups)
	if fixed == 0 || variable == 0 {
		t.Fatalf("the staged corpus covers %d fixed and %d variable roots; the gate must cover both classes", fixed, variable)
	}
	out := runLuauWireFuzzVerdictPipe(t, stageDir, 200, true)
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

const luauFuzzPipeUTF8Anchor = `local function utf8_ok(b: buffer, at: number, n: number): boolean
	local i = at`

// TestLuauWireFuzzVerdictPipeNegativeControl patches the emitted load in the
// staged artifact — one anchor, matched exactly once — so utf8_ok accepts
// ill-formed UTF-8, and requires the verdict pipe to go red on that planted fault.
func TestLuauWireFuzzVerdictPipeNegativeControl(t *testing.T) {
	slowtest.Gate(t, luauFuzzCost+" over the verdict pipe negative control (test/conformance/luau/wirefuzz.luau)")
	sabotaged := "local function utf8_ok(b: buffer, at: number, n: number): boolean\n\tif n >= 0 then return true end\n\tlocal i = at"
	edit := func(src []byte) []byte {
		if n := strings.Count(string(src), luauFuzzPipeUTF8Anchor); n != 1 {
			t.Fatalf("NEGATIVE CONTROL FAILED: the sabotage anchor appears %d times in schema_load.luau, want 1", n)
		}
		return []byte(strings.Replace(string(src), luauFuzzPipeUTF8Anchor, sabotaged, 1))
	}
	stageDir, _ := luauFuzzStagePipeUnits(t, luauFuzzPipeUnits, edit)
	out := runLuauWireFuzzVerdictPipe(t, stageDir, 200, false)
	if !regexp.MustCompile(`FAILED after .*: read acceptance differs: the leg says true, the oracle says false`).MatchString(out) {
		t.Fatalf("NEGATIVE CONTROL FAILED: the verdict pipe went red, but not on an acceptance divergence:\n%s", out)
	}
}
